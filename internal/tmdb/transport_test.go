package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func fastRetries(t *testing.T) {
	old := baseBackoff
	baseBackoff = time.Millisecond
	t.Cleanup(func() { baseBackoff = old })
}

func clientFor(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, _ := New("k")
	c.base = srv.URL
	return c
}

func TestRetriesServerErrors(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":1,"media_type":"movie","title":"Heat"}]}`)
	})
	hits, err := c.Search("heat", "")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestRetriesDroppedConnections(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// Kill the connection mid-request, like a flaky edge does.
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		fmt.Fprint(w, `{"results":[]}`)
	})
	if _, err := c.Search("x", ""); err != nil {
		t.Fatalf("expected retry to recover, got %v", err)
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := c.Search("x", ""); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != maxAttempts {
		t.Errorf("calls = %d, want %d", calls.Load(), maxAttempts)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := c.Health(); err != ErrBadKey {
		t.Fatalf("err = %v, want ErrBadKey", err)
	}
	if calls.Load() != 1 {
		t.Errorf("a 401 should not be retried, calls = %d", calls.Load())
	}
}

func TestEdgeOrderPrefersHealthyAddresses(t *testing.T) {
	e := newEdgeStats()
	e.fail("bad")
	e.fail("bad")
	e.fail("flaky")
	e.ok("good")
	for i := 0; i < 20; i++ { // shuffle must never beat the ranking
		got := e.order([]string{"bad", "flaky", "good", "fresh"})
		if got[0] != "good" || got[1] != "fresh" || got[2] != "flaky" || got[3] != "bad" {
			t.Fatalf("order = %v", got)
		}
	}
}

func TestEdgeStatsPersistAndExpire(t *testing.T) {
	path := t.TempDir() + "/edges.json"
	e := newEdgeStats()
	e.load(path)
	e.ok("1.1.1.1")
	e.fail("2.2.2.2")
	e.edges["3.3.3.3"] = edgeRecord{Fails: 9, Seen: time.Now().Add(-30 * 24 * time.Hour)} // stale
	e.dirty = true
	e.save()

	e2 := newEdgeStats()
	e2.load(path)
	if e2.edges["1.1.1.1"].OKs != 1 || e2.edges["2.2.2.2"].Fails != 1 {
		t.Errorf("stats not restored: %+v", e2.edges)
	}
	if _, ok := e2.edges["3.3.3.3"]; ok {
		t.Error("stale entry should be dropped on load")
	}
}

func TestResolveMergesAndCaches(t *testing.T) {
	e := newEdgeStats()
	calls := 0
	e.lookup = func(_ context.Context, host string) []string {
		calls++
		return []string{"13.224.245.63", "3.175.86.37"}
	}
	for i := 0; i < 3; i++ {
		ips, err := e.resolve(context.Background(), "api.themoviedb.org")
		if err != nil || len(ips) != 2 {
			t.Fatalf("ips=%v err=%v", ips, err)
		}
	}
	if calls != 1 {
		t.Errorf("lookup called %d times, want 1 (cached)", calls)
	}
	if ips, _ := e.resolve(context.Background(), "127.0.0.1"); len(ips) != 1 || ips[0] != "127.0.0.1" {
		t.Errorf("IP literal should pass through, got %v", ips)
	}
}
