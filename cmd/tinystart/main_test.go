package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The whole program under test is run(), so these tests start a real server on
// a real port and talk to it over TCP. That is the only way to make sure of
// things that only exist at this level. It makes sure the configuration
// comes from the environment and a missing secret key stops the process. It
// also makes sure the process opens and migrates the database, and a
// canceled context stops the server without an error.

func TestRunServesUpUntilTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	out := &syncBuffer{}

	done := make(chan error, 1)
	go func() {
		// Port 0 asks the kernel for a free port, so the test never fights
		// another process (or another test) over a fixed one.
		done <- run(ctx, []string{"tinystart"}, testEnv(t, "TINYSTART_ADDR", "127.0.0.1:0"), strings.NewReader(""), out)
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
	// server ever starts. It is the one failure that has to reach main's exit
	// code rather than being logged and forgotten.
	err := run(t.Context(), []string{"tinystart"}, testEnv(t, "TINYSTART_ADDR", "127.0.0.1:99999"), strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("run returned nil for an unbindable address, want an error")
	}
}

// A server that cannot sign a cookie cannot tell a session from a forgery. So
// there is no default to fall back to and no starting without one.
func TestRunRefusesToStartWithoutASecretKey(t *testing.T) {
	env := testEnv(t, "TINYSTART_ADDR", "127.0.0.1:0")
	err := run(t.Context(), []string{"tinystart"}, withoutKey(env), strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("run returned nil with no secret key, want an error")
	}
	if !strings.Contains(err.Error(), "TINYSTART_SECRET_KEY") {
		t.Errorf("error = %v, want it to name TINYSTART_SECRET_KEY", err)
	}
}

// The database path and the environment are the two settings that change what
// the process does rather than only where it listens.
func TestConfigFromEnvDefaults(t *testing.T) {
	cfg := configFromEnv(func(string) string { return "" })

	if cfg.dbPath != "storage/development.sqlite3" {
		t.Errorf("dbPath = %q, want the development database", cfg.dbPath)
	}
	if cfg.production {
		t.Error("production = true with TINYSTART_ENV unset, want false")
	}

	cfg = configFromEnv(mapEnv(map[string]string{
		"TINYSTART_DB":  "/data/production.sqlite3",
		"TINYSTART_ENV": "production",
	}))
	if cfg.dbPath != "/data/production.sqlite3" {
		t.Errorf("dbPath = %q, want the configured path", cfg.dbPath)
	}
	if !cfg.production {
		t.Error("production = false with TINYSTART_ENV=production, want true")
	}
}

// The mailer's address is configurable, so running the binary from a checkout
// can point at a fake and mail nobody. That is what Rails did in development
// with letter_opener. Unset is the real API.
func TestConfigFromEnvPostmarkURL(t *testing.T) {
	if url := configFromEnv(func(string) string { return "" }).postmarkURL; url != "" {
		t.Errorf("postmarkURL = %q with nothing set, want empty", url)
	}

	cfg := configFromEnv(mapEnv(map[string]string{"POSTMARK_API_URL": "http://127.0.0.1:3098"}))
	if cfg.postmarkURL != "http://127.0.0.1:3098" {
		t.Errorf("postmarkURL = %q, want the configured address", cfg.postmarkURL)
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
			got := configFromEnv(mapEnv(map[string]string{"TINYSTART_ADDR": tt.addr}))
			if got.addr != tt.wantAddr {
				t.Errorf("addr = %q, want %q", got.addr, tt.wantAddr)
			}
		})
	}
}

// testEnv is the smallest environment run will start in: a secret key and its
// own database in a directory the test framework cleans up. It also adds
// whatever else the caller wants.
func testEnv(t *testing.T, extra ...string) func(string) string {
	t.Helper()
	if len(extra)%2 != 0 {
		t.Fatal("testEnv takes key/value pairs")
	}

	env := map[string]string{
		"TINYSTART_SECRET_KEY": strings.Repeat("k", 32),
		"TINYSTART_DB":         filepath.Join(t.TempDir(), "test.sqlite3"),
	}
	for i := 0; i < len(extra); i += 2 {
		env[extra[i]] = extra[i+1]
	}
	return mapEnv(env)
}

// withoutKey takes the secret key back out of an environment.
func withoutKey(getenv func(string) string) func(string) string {
	return func(key string) string {
		if key == "TINYSTART_SECRET_KEY" {
			return ""
		}
		return getenv(key)
	}
}

// mapEnv turns a map into a getenv: anything not in it is unset, which is
// what run sees in a container with nothing configured.
func mapEnv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// waitForAddr reads the address the server actually bound out of its own log.
// run logs it precisely so that a caller who asked for port 0 can find out
// where the server ended up. The test is that caller.
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

// syncBuffer is a bytes.Buffer the test can read while the server goroutine
// still writes to it. bytes.Buffer alone is not safe for that, and the race
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

// set-password is the replacement for `bin/rails console`: the way to get
// back into an account when there is no working mail. That covers a laptop,
// or `kamal app exec`. The password comes in on stdin so it never sits in a
// shell history or a process list.
func TestSetPasswordChangesThePassword(t *testing.T) {
	restore := store.UseCheapPasswordHashing()
	defer restore()

	env := testEnv(t)
	db := openTestDB(t, env("TINYSTART_DB"))
	if _, err := db.CreateUser(t.Context(), "jaime@example.com", "old-password"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := run(t.Context(), []string{"tinystart", "set-password", "jaime@example.com"}, env,
		strings.NewReader("new-password\n"), &out)
	if err != nil {
		t.Fatalf("set-password: %v", err)
	}
	if !strings.Contains(out.String(), "jaime@example.com") {
		t.Errorf("stdout = %q, want it to name the account", out.String())
	}

	if _, err := db.Authenticate(t.Context(), "jaime@example.com", "new-password"); err != nil {
		t.Errorf("the new password does not authenticate: %v", err)
	}
	if _, err := db.Authenticate(t.Context(), "jaime@example.com", "old-password"); err == nil {
		t.Error("the old password still authenticates")
	}
}

func TestSetPasswordRefusesWhatItCannotDo(t *testing.T) {
	restore := store.UseCheapPasswordHashing()
	defer restore()

	env := testEnv(t)
	db := openTestDB(t, env("TINYSTART_DB"))
	if _, err := db.CreateUser(t.Context(), "jaime@example.com", "old-password"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"unknown email", []string{"tinystart", "set-password", "nobody@example.com"}, "x\n", "nobody@example.com"},
		{"missing email", []string{"tinystart", "set-password"}, "x\n", "usage"},
		{"empty password", []string{"tinystart", "set-password", "jaime@example.com"}, "\n", "empty"},
		{"unknown command", []string{"tinystart", "frobnicate"}, "", "frobnicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(t.Context(), tt.args, env, strings.NewReader(tt.stdin), io.Discard)
			if err == nil {
				t.Fatal("run returned nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
	if _, err := db.Authenticate(t.Context(), "jaime@example.com", "old-password"); err != nil {
		t.Errorf("a refused set-password changed the password: %v", err)
	}
}

// openTestDB opens the same file run will, so a test can seed it and read it
// back through the store rather than raw SQL.
func openTestDB(t *testing.T, path string) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
