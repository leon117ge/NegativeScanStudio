package hikcam

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestControllerStopBeforeStartAndNonBlockingBump(t *testing.T) {
	var c Controller
	var active atomic.Int32
	var overlaps atomic.Int32
	var started1, stopped1 atomic.Bool
	releaseGrab := make(chan struct{})

	g1 := c.Bump()
	c.Do(func() {
		c.StartLoop(g1, func(valid func() bool) {
			if active.Add(1) != 1 {
				overlaps.Add(1)
			}
			defer func() {
				active.Add(-1)
				stopped1.Store(true)
			}()
			started1.Store(true)
			<-releaseGrab
			_ = valid()
		})
	})

	// Wait for loop 1 to enter its simulated slow grab.
	deadline := time.Now().Add(2 * time.Second)
	for !started1.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !started1.Load() {
		t.Fatal("loop 1 did not start")
	}

	// Bump from caller (UI thread) must return immediately without waiting on releaseGrab.
	start := time.Now()
	g2 := c.Bump()
	c.Bump() // simulate rapid tab switch cancelling g2
	g3 := c.Bump()
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("Bump blocked caller for %v", time.Since(start))
	}

	var ran2, ran3 atomic.Bool
	done3 := make(chan struct{})
	c.Do(func() {
		// Stale g2 must not launch.
		if c.StartLoop(g2, func(valid func() bool) { ran2.Store(true) }) {
			t.Errorf("stale generation g2 started unexpectedly")
		}
	})
	c.Do(func() {
		c.StartLoop(g3, func(valid func() bool) {
			if !stopped1.Load() {
				overlaps.Add(1)
			}
			if active.Add(1) != 1 {
				overlaps.Add(1)
			}
			defer active.Add(-1)
			ran3.Store(true)
			close(done3)
		})
	})

	// Release slow grab of loop 1; loop 3 must only run after loop 1 fully stopped.
	close(releaseGrab)
	select {
	case <-done3:
	case <-time.After(2 * time.Second):
		t.Fatal("loop 3 did not finish")
	}

	if ran2.Load() {
		t.Fatal("stale g2 should have been skipped")
	}
	if !ran3.Load() {
		t.Fatal("g3 should have run")
	}
	if overlaps.Load() != 0 {
		t.Fatalf("detected %d overlapping loops", overlaps.Load())
	}
}

func TestControllerConcurrentStopSync(t *testing.T) {
	var c Controller
	var mu sync.Mutex
	var events []string
	record := func(s string) {
		mu.Lock()
		events = append(events, s)
		mu.Unlock()
	}

	g := c.Bump()
	entered := make(chan struct{})
	c.Do(func() {
		c.StartLoop(g, func(valid func() bool) {
			record("start")
			close(entered)
			for valid() {
				time.Sleep(5 * time.Millisecond)
			}
			time.Sleep(30 * time.Millisecond) // simulate StopGrab() cleanup
			record("stopgrab")
		})
	})
	<-entered

	applied := make(chan struct{})
	c.Do(func() {
		c.StopSync()
		record("apply")
		close(applied)
	})
	<-applied

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 || events[0] != "start" || events[1] != "stopgrab" || events[2] != "apply" {
		t.Fatalf("unexpected sequence: %v", events)
	}
}
