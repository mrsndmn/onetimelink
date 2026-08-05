package misc

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Limiter is a token bucket rate limiter keyed by client address. It bounds
// how fast a single client may create secrets or guess auth tokens.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens refilled per second
	burst    float64
	lastGC   time.Time
	nowFunc  func() time.Time
	maxKeys  int
	gcPeriod time.Duration
}

type bucket struct {
	tokens float64
	seen   time.Time
}

// NewLimiter returns a limiter allowing burst requests immediately and then
// one request every interval.
func NewLimiter(burst int, interval time.Duration) *Limiter {
	if burst < 1 {
		burst = 1
	}
	if interval <= 0 {
		interval = time.Second
	}
	return &Limiter{
		buckets:  make(map[string]*bucket),
		rate:     1 / interval.Seconds(),
		burst:    float64(burst),
		nowFunc:  time.Now,
		maxKeys:  10000,
		gcPeriod: time.Minute,
	}
}

// Allow reports whether a request from key may proceed.
func (l *Limiter) Allow(key string) bool {
	now := l.nowFunc()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.gc(now)

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			// Table is full of unknown clients; fail closed rather than
			// letting an attacker grow it without bound.
			return false
		}
		b = &bucket{tokens: l.burst}
		l.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.seen).Seconds() * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}
	b.seen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gc drops buckets that have been full (i.e. idle) for a while. Caller holds
// the lock.
func (l *Limiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < l.gcPeriod {
		return
	}
	l.lastGC = now
	idle := time.Duration(l.burst/l.rate) * time.Second
	for k, b := range l.buckets {
		if now.Sub(b.seen) > idle {
			delete(l.buckets, k)
		}
	}
}

// ClientKey returns the key a limiter should use for a request: the peer
// address, which (unlike the proxy headers) cannot be forged by the client.
func ClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimit wraps a handler, rejecting requests over the limit with 429.
func RateLimit(l *Limiter, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(ClientKey(r)) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		h.ServeHTTP(w, r)
	})
}
