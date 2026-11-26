// Package hdrctl is the toolkit-free controller behind the macOS Camera and
// White light scan pages. It ports the HDRUI logic of ../../hdrpage.go
// (connect, live view, status poll, meter, test shot, capture and merge,
// Stop) with the same calls into camera and hdrscan; only the widget plumbing
// differs.
//
// Threading: every method is called on the UI goroutine. Camera calls run on
// one worker goroutine (camWorker, as on Windows); merges run on their own
// goroutine. Results come back through Host.Post, which runs the function on
// the UI goroutine and redraws. Nothing here ever runs on, or blocks, the
// macOS main thread: Gio's app.Main owns it and keeps the main run loop
// turning, which ImageCaptureCore callbacks need.
package hdrctl

import (
	"fmt"
	"runtime/debug"
)

type camWorker struct {
	jobs    chan func()
	onPanic func(r any, stack string)
}

func newCamWorker() *camWorker {
	w := &camWorker{jobs: make(chan func(), 64)}
	go func() {
		// Unlike Windows (COM apartment), macOS camera transports need no
		// locked thread; one goroutine keeps camera calls serialised.
		for j := range w.jobs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						st := string(debug.Stack())
						fmt.Println("camera job panic:", r)
						if w.onPanic != nil {
							w.onPanic(r, st)
						}
					}
				}()
				j()
			}()
		}
	}()
	return w
}

func (w *camWorker) do(f func()) { w.jobs <- f }
