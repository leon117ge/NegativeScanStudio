//go:build darwin || (linux && gio)

// Command macapp is NegativeScan Studio for macOS: the Gio user interface
// over the same core packages as the Windows app (which keeps its walk UI;
// this module is not part of the Windows build).
//
// Threading model (see docs/MAC_UI.md):
//   - main() hands the main thread to app.Main, which runs the Cocoa
//     NSApplication run loop. Nothing else ever runs on, or blocks, the main
//     thread, so main-queue blocks and run-loop sources (ImageCaptureCore
//     delegate callbacks, file panels) are serviced at all times.
//   - One window goroutine owns all UI state: it lays out frames and runs
//     the functions posted by background work (UI.Post + Window.Invalidate).
//   - Camera I/O runs on the camera worker goroutine (hdrctl), merges on
//     their own goroutines; they report back only through UI.Post.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"basetool/macapp/appstate"
	"basetool/macapp/crashlog"
)

func main() {
	crashlog.Install(appstate.AppVersion)
	defer crashlog.Main()
	debug.SetGCPercent(20) // as on Windows: large RAW buffers
	selftest := len(os.Args) > 1 && os.Args[1] == "-selftest-runloop"
	if selftest {
		go func() {
			time.Sleep(60 * time.Second)
			fmt.Println("runloop selftest: FAIL: no frame within 60 s")
			os.Exit(2)
		}()
	}
	go func() {
		defer crashlog.Main()
		w := new(app.Window)
		w.Option(app.Title(appstate.AppVersion), app.Size(unit.Dp(1280), unit.Dp(800)), app.MinSize(unit.Dp(1024), unit.Dp(640)))
		code := 0
		if err := run(w, selftest); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
		os.Exit(code)
	}()
	app.Main()
}

func run(w *app.Window, selftest bool) error {
	th := newTheme()
	u := newUI(th, w.Invalidate)
	var args []string
	if len(os.Args) > 1 {
		args = os.Args[1:]
	}
	u.nav, _ = appstate.FromArgs(args)
	u.setNav()
	u.hdr.Start()
	var probe chan error
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			u.drain()
			u.hdr.Shutdown() // switch the light off and disconnect (Windows: lightui.shutdown on Closing)
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			func() {
				defer crashlog.Guard("frame", func(r any) { u.Message("Internal error", fmt.Sprintf("%v\n\nDetails: %s", r, crashlog.File())) })
				u.Layout(gtx)
			}()
			e.Frame(gtx.Ops)
			if selftest && probe == nil {
				probe = make(chan error, 1)
				go func() { probe <- runLoopProbe(5 * time.Second) }()
				go func() {
					err := <-probe
					if err != nil {
						fmt.Println("runloop selftest: FAIL:", err)
						os.Exit(1)
					}
					fmt.Println("runloop selftest: PASS")
					os.Exit(0)
				}()
			}
		}
	}
}
