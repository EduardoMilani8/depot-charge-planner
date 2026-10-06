// Command lab serves the simulation laboratory (web interface) on a local address.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EduardoMilani8/depot-charge-planner/internal/lab"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// checkAddr accepts only loopback addresses with an explicit host: the lab has no login.
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("endereço inválido %q: use host:porta, por exemplo 127.0.0.1:8080", addr)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("o laboratório só escuta em endereço local (127.0.0.1, ::1 ou localhost): ele não tem login")
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lab", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on (loopback only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if err := checkAddr(*addr); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	srv := &http.Server{
		Handler:           lab.New().Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
	}
	fmt.Fprintf(stdout, "Laboratório em http://%s (Ctrl+C para encerrar)\n", ln.Addr())
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
