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

// The whole program under test is run(), so these tests start a real server on
// a real port and talk to it over TCP. That is cheap here — the process has no
// database and no templates yet — and it is the only way to check the two
// things that only exist at this level: that the address comes from the
// environment, and that a cancelled context stops the server without an error.

func TestRunServesUpUntilTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	out := &syncBuffer{}

	done := make(chan error, 1)
	go func() {
		// Port 0 asks the kernel for a free port, so the test never fights
		// another process (or another test) over a fixed one.
		done <- run(ctx, []string{"tinystart"}, envWith("TINYSTART_ADDR", "127.0.0.1:0"), out)
	}()

	addr := waitForAddr(t, out)

	resp, err := http.Get("http://" + addr + "/up")
	if err != nil {
		t.Fatalf("GET /up: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /up status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if got := string(bytes.TrimSpace(body)); got != "ok" {
		t.Errorf("GET /up body = %q, want %q", got, "ok")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v, want nil after a cancelled context", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return within 10s of the context being cancelled")
	}
}

func TestRunReturnsAnErrorWhenTheAddressCannotBeBound(t *testing.T) {
	// 99999 is past the top of the port range, so listening fails before the
	// server ever starts — the one failure that has to reach main's exit code
	// rather than being logged and forgotten.
	err := run(t.Context(), []string{"tinystart"}, envWith("TINYSTART_ADDR", "127.0.0.1:99999"), io.Discard)
	if err == nil {
		t.Fatal("run returned nil for an unbindable address, want an error")
	}
}

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantAddr string
	}{
		{name: "unset falls back to the development port", addr: "", wantAddr: ":3000"},
		{name: "set is taken verbatim", addr: "0.0.0.0:80", wantAddr: "0.0.0.0:80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configFromEnv(envWith("TINYSTART_ADDR", tt.addr))
			if got.addr != tt.wantAddr {
				t.Errorf("addr = %q, want %q", got.addr, tt.wantAddr)
			}
		})
	}
}

// envWith is a getenv that knows one variable and reports every other as
// unset, which is what run sees in a container with nothing configured.
func envWith(key, value string) func(string) string {
	return func(k string) string {
		if k == key {
			return value
		}
		return ""
	}
}

// waitForAddr reads the address the server actually bound out of its own log.
// run logs it precisely so that a caller who asked for port 0 can find out
// where the server ended up; the test is that caller.
func waitForAddr(t *testing.T, out *syncBuffer) string {
	t.Helper()

	listening := regexp.MustCompile(`addr=(\S+)`)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := listening.FindStringSubmatch(out.String()); m != nil {
			return m[1]
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("server never logged an address; log so far:\n%s", out.String())
	return ""
}

// syncBuffer is a bytes.Buffer the test can read while the server goroutine is
// still writing to it. bytes.Buffer alone is not safe for that, and the race
// detector says so.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
