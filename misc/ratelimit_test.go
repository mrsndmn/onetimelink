package misc

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestLimiterBurstThenBlock(t *testing.T) {
	l := NewLimiter(3, time.Second)
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d within burst was rejected", i)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("request past the burst was allowed")
	}
	// A different client has its own bucket.
	if !l.Allow("5.6.7.8") {
		t.Error("unrelated client was rate limited")
	}
}

func TestLimiterRefills(t *testing.T) {
	l := NewLimiter(1, 100*time.Millisecond)
	now := time.Now()
	l.nowFunc = func() time.Time { return now }

	if !l.Allow("c") {
		t.Fatal("first request rejected")
	}
	if l.Allow("c") {
		t.Fatal("second immediate request allowed")
	}
	now = now.Add(150 * time.Millisecond)
	if !l.Allow("c") {
		t.Error("bucket did not refill over time")
	}
}

func TestLimiterIsConcurrencySafe(t *testing.T) {
	l := NewLimiter(1000000, time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Allow("shared")
				l.Allow(string(rune('a' + i%26)))
			}
		}(i)
	}
	wg.Wait()
}

func TestRateLimitMiddleware(t *testing.T) {
	l := NewLimiter(2, time.Hour)
	h := RateLimit(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	codes := []int{}
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("POST", "/api/v1/new", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		codes = append(codes, rr.Code)
	}
	want := []int{200, 200, 429, 429}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("request %d: got %d, wanted %d (all: %v)", i, codes[i], want[i], codes)
		}
	}
}

// The limiter must key on the peer address, not on headers the client
// controls, otherwise it is bypassed by spoofing X-Forwarded-For.
func TestClientKeyIgnoresProxyHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/g", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	if got := ClientKey(req); got != "10.0.0.1" {
		t.Errorf("got %v, wanted 10.0.0.1", got)
	}
}
