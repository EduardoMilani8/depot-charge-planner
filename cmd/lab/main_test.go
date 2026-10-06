package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestCheckAddr(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:8080", "localhost:9000", "[::1]:8080", "127.0.0.1:0"} {
		if err := checkAddr(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"0.0.0.0:8080", ":8080", "192.168.0.10:8080", "example.com:80", "8080", "[::]:8080"} {
		if err := checkAddr(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestRunServesAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut syncBuf
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-addr", "127.0.0.1:0"}, &out, &errOut) }()
	url := regexp.MustCompile(`http://[^\s]+`)
	var base string
	for i := 0; i < 100 && base == ""; i++ {
		base = url.FindString(out.String())
		time.Sleep(20 * time.Millisecond)
	}
	if base == "" {
		cancel()
		t.Fatalf("no URL printed; stderr: %s", errOut.String())
	}
	resp, err := http.Get(base + "/api/defaults")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status %d", resp.StatusCode)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit %d: %s", code, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop after the context was cancelled")
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	var out, errOut syncBuf
	if code := run(context.Background(), []string{"-addr", "0.0.0.0:8080"}, &out, &errOut); code != 2 {
		t.Errorf("non-loopback address: exit %d", code)
	}
	if code := run(context.Background(), []string{"extra"}, &out, &errOut); code != 2 {
		t.Errorf("positional argument: exit %d", code)
	}
	if code := run(context.Background(), []string{"-nonsense"}, &out, &errOut); code != 2 {
		t.Errorf("unknown flag: exit %d", code)
	}
}
