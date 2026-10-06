// Package lab is the HTTP server of the simulation laboratory: three JSON routes plus
// the embedded web interface. It holds no planning logic.
package lab

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/web"
)

const (
	maxBody       = 64 << 10
	maxConcurrent = 2
)

type Server struct {
	sem     chan struct{} // one token per simulation in progress
	static  http.Handler
	timeout time.Duration
}

func New() *Server {
	sub, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return &Server{
		sem:     make(chan struct{}, maxConcurrent),
		static:  noListing(sub, http.FileServer(http.FS(sub))),
		timeout: 60 * time.Second,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/defaults", s.route(http.MethodGet, s.defaults))
	mux.HandleFunc("/api/compare", s.route(http.MethodPost, s.compare))
	mux.HandleFunc("/api/run", s.route(http.MethodPost, s.run))
	mux.Handle("/", s.static)
	return guard(mux)
}

// noListing serves files only: a path ending in "/" (other than "/" itself) or naming a
// directory answers 404 instead of a directory listing.
func noListing(fsys fs.FS, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if p != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(p, "/")
		if name == "" {
			name = "."
		}
		if st, err := fs.Stat(fsys, name); err == nil && st.IsDir() {
			if _, err := fs.Stat(fsys, path.Join(name, "index.html")); err != nil || name != "." {
				http.NotFound(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isLocalHost accepts 127.0.0.1, localhost and ::1 with or without a port.
func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// guard rejects requests for a non-local Host (DNS rebinding) or from a foreign Origin,
// and marks every response as uncacheable.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			writeError(w, &apiError{status: http.StatusForbidden, Message: "Acesso negado: o laboratório só atende endereços locais."})
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			// The page and its API share one origin: the Origin's host must be the Host.
			if u, err := url.Parse(o); err != nil || !strings.EqualFold(u.Host, r.Host) {
				writeError(w, &apiError{status: http.StatusForbidden, Message: "Acesso negado: origem não permitida."})
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

type handlerFunc func(r *http.Request) (any, *apiError)

// route wraps a JSON handler: method, content type, body limit, response encoding.
func (s *Server) route(method string, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, &apiError{status: http.StatusMethodNotAllowed, Message: "Método não permitido."})
			return
		}
		if method == http.MethodPost {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				writeError(w, &apiError{status: http.StatusUnsupportedMediaType, Message: "Envie o corpo como application/json."})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		body, aerr := h(r)
		if aerr != nil {
			writeError(w, aerr)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// decodeBody reads a JSON object into v (an empty body leaves v unchanged). Unknown
// fields, wrong types, trailing data and oversized bodies are errors.
func decodeBody(r *http.Request, v any) *apiError {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &apiError{status: http.StatusRequestEntityTooLarge, Message: "Corpo da requisição grande demais (limite de 64 KB)."}
		}
		return &apiError{status: http.StatusBadRequest, Message: "JSON inválido: " + err.Error()}
	}
	// Nothing but whitespace may follow the object.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return &apiError{status: http.StatusRequestEntityTooLarge, Message: "Corpo da requisição grande demais (limite de 64 KB)."}
		}
		return &apiError{status: http.StatusBadRequest, Message: "JSON inválido: há dados depois do objeto."}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		writeError(w, &apiError{status: http.StatusInternalServerError, Message: "Erro interno ao montar a resposta."})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func writeError(w http.ResponseWriter, e *apiError) {
	writeJSON(w, e.status, e)
}

var errBusy = &apiError{status: http.StatusServiceUnavailable,
	Message: "Servidor ocupado: já há simulações em andamento. Tente de novo em instantes."}

// acquire reserves a simulation slot without waiting.
func (s *Server) acquire() (release func(), ok bool) {
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, true
	default:
		return nil, false
	}
}
