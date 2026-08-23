package web

import (
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestLimiterAllowsUpToTheLimitAndThenStops(t *testing.T) {
	clock := &testClock{now: time.Unix(0, 0)}
	l := newLimiter(3, time.Minute, clock.Now)

	for i := range 3 {
		if !l.allow("10.0.0.1") {
			t.Fatalf("attempt %d was refused, want the first 3 allowed", i+1)
		}
	}
	if l.allow("10.0.0.1") {
		t.Error("attempt 4 was allowed, want it refused")
	}
}

func TestLimiterCountsEachAddressSeparately(t *testing.T) {
	clock := &testClock{now: time.Unix(0, 0)}
	l := newLimiter(1, time.Minute, clock.Now)

	if !l.allow("10.0.0.1") {
		t.Fatal("the first address was refused on its first attempt")
	}
	if !l.allow("10.0.0.2") {
		t.Error("a second address was refused because the first had used its allowance")
	}
}

func TestLimiterStartsAFreshWindow(t *testing.T) {
	clock := &testClock{now: time.Unix(0, 0)}
	l := newLimiter(1, time.Minute, clock.Now)

	l.allow("10.0.0.1")
	if l.allow("10.0.0.1") {
		t.Fatal("a second attempt inside the window was allowed")
	}

	clock.advance(time.Minute)

	if !l.allow("10.0.0.1") {
		t.Error("the window did not reset after it had run out")
	}
}

// The map must not grow forever. Nothing prunes it on a timer. The next
// request past a stale entry drops it.
func TestLimiterForgetsOldWindows(t *testing.T) {
	clock := &testClock{now: time.Unix(0, 0)}
	l := newLimiter(1, time.Minute, clock.Now)

	l.allow("10.0.0.1")
	clock.advance(time.Minute)
	l.allow("10.0.0.2")

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, still := l.windows["10.0.0.1"]; still {
		t.Error("a window that had run out was still in the map")
	}
}

// Every request goroutine shares the limiter, so it has to be safe for them.
// -race is what makes this test worth anything.
func TestLimiterIsSafeForConcurrentUse(t *testing.T) {
	l := newLimiter(100, time.Minute, time.Now)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.allow("10.0.0." + string(rune('0'+i%10)))
		}()
	}
	wg.Wait()
}

// The two policies, as they are wired: sign-in is 10 in 3 minutes and sign-up
// is 2 in 5, and both answer the same way.
func TestSignInIsRateLimited(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	credentials := url.Values{"email": {"one@example.com"}, "password": {"wrong"}}
	for range signInLimit {
		ts.post("/session", credentials).assertStatus(http.StatusSeeOther)
	}

	ts.post("/session", credentials).assertRedirect("/session/new")
	ts.get("/session/new").assertContains("Try again later.")

	// And the window runs out.
	ts.clock.advance(signInWindow)
	ts.post("/session", credentials)
	ts.get("/session/new").assertContains("Try another email address or password.")
}

func TestSignUpIsRateLimited(t *testing.T) {
	ts := newTestServer(t)

	for i := range signUpLimit {
		ts.post("/sign_up", form(
			"user[email]", string(rune('a'+i))+"@example.com",
			"user[password]", "password",
		)).assertStatus(http.StatusSeeOther)
	}

	ts.post("/sign_up", form("user[email]", "z@example.com", "user[password]", "password")).
		assertRedirect("/session/new")
	ts.get("/session/new").assertContains("Try again later.")
}
