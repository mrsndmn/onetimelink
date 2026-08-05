package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestConcurrentAccess hammers the store from many goroutines at once while
// the expiry sweep runs concurrently.
//
// This is the regression test for the bug that made the original store a bare
// map with no synchronisation: request handlers and the expiry goroutine wrote
// to it at the same time, which the Go runtime aborts with
// "fatal error: concurrent map writes" — killing the process and, since
// nothing is persisted, destroying every outstanding secret.
//
// Run with -race to have the detector confirm it.
func TestConcurrentAccess(t *testing.T) {
	st := New(100000)

	const workers = 50
	const iterations = 200

	var wg sync.WaitGroup
	var sweeper sync.WaitGroup
	stop := make(chan struct{})

	// An expiry sweep running in parallel with the writers, which is exactly
	// the pair the race detector flagged in the original code.
	sweeper.Add(1)
	go func() {
		defer sweeper.Done()
		for {
			select {
			case <-stop:
				return
			default:
				st.expireOnce(time.Now())
				// Yield: a tight sweep would hold the write lock
				// permanently and starve the workers.
				time.Sleep(time.Microsecond)
			}
		}
	}()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id, err := st.NewEntry(fmt.Sprintf("secret-%d-%d", w, i), 2, 1, "auth@example.org", "")
				if err != nil {
					t.Errorf("NewEntry: %v", err)
					return
				}
				st.GetEntry(id)
				st.GetEntryInfoHidden(id, "b", "/g", "/a/")
				st.Claim(id, "b", "/g", "/a/")
				st.Claim(id, "b", "/g", "/a/")
				st.Len()
			}
		}(w)
	}

	wg.Wait()
	close(stop)
	sweeper.Wait()
}

// TestClaimIsAtomic checks that a one-time link stays one-time when several
// clients open it simultaneously.
//
// Looking the entry up and consuming the click used to be two separate
// operations, so racing requests could each read a max_clicks=1 secret before
// any of them deleted it.
func TestClaimIsAtomic(t *testing.T) {
	const trials = 200
	const readers = 16

	for trial := 0; trial < trials; trial++ {
		st := New(0)
		mustAdd(t, st, "one-time-secret", 1, 1, "auth@example.org", "theid")

		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		served := 0

		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, ok := st.Claim("theid", "b", "/g", "/a/"); ok {
					mu.Lock()
					served++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()

		if served != 1 {
			t.Fatalf("trial %d: a max_clicks=1 secret was served %d times, wanted exactly 1",
				trial, served)
		}
	}
}

// The same property for a secret allowed a limited number of views.
func TestClaimRespectsMaxClicksUnderLoad(t *testing.T) {
	const maxClicks = 5
	const readers = 40

	for trial := 0; trial < 100; trial++ {
		st := New(0)
		mustAdd(t, st, "limited", maxClicks, 1, "auth@example.org", "theid")

		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		served := 0

		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, ok := st.Claim("theid", "b", "/g", "/a/"); ok {
					mu.Lock()
					served++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()

		if served != maxClicks {
			t.Fatalf("trial %d: secret served %d times, wanted %d", trial, served, maxClicks)
		}
	}
}
