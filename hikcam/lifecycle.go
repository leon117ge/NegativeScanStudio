package hikcam

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Controller serializes camera lifecycle operations (enumerate, open, close,
// apply, capture, preview start/stop) on one OS-thread-locked worker so the UI
// thread never blocks waiting on camera I/O or an exiting grab loop.
type Controller struct {
	OnPanic func(r any, stack string)

	once sync.Once
	jobs chan func()

	gen atomic.Int64

	doneMu sync.Mutex
	done   chan struct{}
}

func (c *Controller) init() {
	c.once.Do(func() {
		c.jobs = make(chan func(), 64)
		go func() {
			runtime.LockOSThread()
			for j := range c.jobs {
				func() {
					defer func() {
						if r := recover(); r != nil {
							st := string(debug.Stack())
							fmt.Println("hikcam job panic:", r)
							if c.OnPanic != nil {
								c.OnPanic(r, st)
							}
						}
					}()
					j()
				}()
			}
		}()
	})
}

// Do enqueues f to run on the serialized camera worker. Safe from any
// goroutine, including the UI thread; it never blocks on an active grab loop.
func (c *Controller) Do(f func()) {
	c.init()
	c.jobs <- f
}

// Bump invalidates any active or queued preview generation immediately without
// waiting for the grab loop to exit. Safe on the UI thread.
func (c *Controller) Bump() int64 {
	return c.gen.Add(1)
}

// Gen returns the current preview generation.
func (c *Controller) Gen() int64 {
	return c.gen.Load()
}

// StopSync signals any running preview loop to exit and blocks until its
// goroutine (including StopGrab cleanup) has returned. Must be called from a
// worker/background goroutine, never the UI thread.
func (c *Controller) StopSync() {
	c.gen.Add(1)
	c.doneMu.Lock()
	ch := c.done
	c.done = nil
	c.doneMu.Unlock()
	if ch != nil {
		<-ch
	}
}

// StartLoop ensures any previous preview loop has stopped, then starts fn in a
// background goroutine if gen is still current. Must be called from the worker
// goroutine (after StopSync if replacing a loop).
func (c *Controller) StartLoop(gen int64, fn func(valid func() bool)) bool {
	c.StopSyncIfNeeded()
	c.doneMu.Lock()
	if c.gen.Load() != gen {
		c.doneMu.Unlock()
		return false
	}
	done := make(chan struct{})
	c.done = done
	c.doneMu.Unlock()
	valid := func() bool { return c.gen.Load() == gen }
	go func() {
		defer close(done)
		fn(valid)
	}()
	return true
}

// StopSyncIfNeeded waits for any previously launched loop to finish without
// bumping gen (so the caller's own freshly bumped gen stays valid).
func (c *Controller) StopSyncIfNeeded() {
	c.doneMu.Lock()
	ch := c.done
	c.done = nil
	c.doneMu.Unlock()
	if ch != nil {
		<-ch
	}
}
