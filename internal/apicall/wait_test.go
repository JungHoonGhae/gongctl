package apicall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JungHoonGhae/gongctl/internal/fetch"
)

// serveAfter answers 403 for the first n requests, then 200 — the gateway's actual
// behaviour after an approval.
func serveAfter(t *testing.T, n int32) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) <= n {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestCallWaitingRetriesUntilPropagated(t *testing.T) {
	srv, hits := serveAfter(t, 2)
	old := pollFor(t, 10*time.Millisecond)
	defer old()

	res, err := CallWaiting(context.Background(), fetch.New(fetch.WithDelay(0)),
		srv.URL, nil, "k", time.Second, nil)
	if err != nil {
		t.Fatalf("should have succeeded once the gateway caught up: %v", err)
	}
	if res.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", res.Status)
	}
	if got := atomic.LoadInt32(hits); got != 3 {
		t.Errorf("requests = %d, want 3 (two refusals then success)", got)
	}
}

// Running out of time must report the wait it actually performed and stay
// recognisable as propagation, so a caller can tell it apart from a bad key.
func TestCallWaitingGivesUpWithPropagationError(t *testing.T) {
	srv, hits := serveAfter(t, 1000)
	old := pollFor(t, 10*time.Millisecond)
	defer old()

	_, err := CallWaiting(context.Background(), fetch.New(fetch.WithDelay(0)),
		srv.URL, nil, "k", 35*time.Millisecond, nil)
	if !errors.Is(err, ErrPropagating) {
		t.Fatalf("err = %v, want ErrPropagating", err)
	}
	if atomic.LoadInt32(hits) < 2 {
		t.Errorf("should have retried before giving up, tried %d", atomic.LoadInt32(hits))
	}
}

// Anything other than a gateway 403 must fail on the first attempt: a rejected key
// does not improve by being asked again, and retrying would bury the real reason.
func TestCallWaitingDoesNotRetryOtherErrors(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`<error>SERVICE_KEY_IS_NOT_REGISTERED_ERROR</error>`))
	}))
	defer srv.Close()

	_, err := CallWaiting(context.Background(), fetch.New(fetch.WithDelay(0)),
		srv.URL, nil, "k", time.Minute, nil)
	if !errors.Is(err, ErrKeyRejected) {
		t.Fatalf("err = %v, want ErrKeyRejected", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("requests = %d, want 1 — a rejected key must not be retried", got)
	}
}

func TestCallWaitingCapsTheWait(t *testing.T) {
	if MaxPropagationWait > time.Hour {
		t.Errorf("MaxPropagationWait = %v; waiting beyond the portal's own ceiling is not waiting for propagation", MaxPropagationWait)
	}
}

// pollFor shortens the retry interval for tests and returns a restore func.
func pollFor(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := propagationPoll
	propagationPoll = d
	return func() { propagationPoll = old }
}
