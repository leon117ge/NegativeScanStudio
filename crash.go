package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/lxn/walk"
)

// crashFile collects panics: recovered ones (written by guard) and fatal
// ones the runtime prints on its way out (debug.SetCrashOutput). A Windows
// GUI program has no console, so without this a crash leaves no trace.
func crashFile() string { return filepath.Join(configDir(), "crash_log.txt") }

var crashMu sync.Mutex

func appendCrash(text string) {
	crashMu.Lock()
	defer crashMu.Unlock()
	_ = os.MkdirAll(configDir(), 0755)
	f, err := os.OpenFile(crashFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "==== %s %s (formerly %s) ====\n%s\n", time.Now().Format("2006-01-02 15:04:05.000"), appVersion, legacyAppName, text)
}

// installCrashLogger routes unrecovered panics and fatal runtime errors to
// crash_log.txt (append mode: the file is only written when something dies).
func installCrashLogger() {
	_ = os.MkdirAll(configDir(), 0755)
	f, err := os.OpenFile(crashFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	f.Close() // SetCrashOutput keeps its own duplicate
}

// mainCrash is deferred in main: it records a panic that reached the top
// and re-raises it (the runtime then also writes it via SetCrashOutput).
func mainCrash() {
	if r := recover(); r != nil {
		appendCrash(fmt.Sprintf("unrecovered panic in main: %v\n%s", r, debug.Stack()))
		panic(r)
	}
}

// guard is deferred around UI callbacks and camera jobs: it logs the panic
// with its stack to camera_log.txt and crash_log.txt and shows an error
// instead of letting the program exit.
func (u *HDRUI) guard(where string) {
	r := recover()
	if r == nil {
		return
	}
	stack := string(debug.Stack())
	u.reportPanic(where, r, stack)
}

func (u *HDRUI) reportPanic(where string, r any, stack string) {
	msg := fmt.Sprintf("PANIC in %s: %v", where, r)
	if u.log != nil {
		u.log.Printf("%s\n%s", msg, stack)
	}
	appendCrash(msg + "\n" + stack)
	show := func() {
		defer func() { recover() }()
		u.busy = false
		u.updateButtons()
		func() { defer func() { recover() }(); u.resumeLiveView() }()
		logPath := crashFile()
		if u.log != nil {
			logPath = u.log.Path()
		}
		walk.MsgBox(u.win(), appVersion+" - internal error",
			"An internal error stopped this action:\n\n"+fmt.Sprint(r)+"\n\nWhere: "+where+
				"\n\nThe program keeps running. Details (with the stack) are in\n"+logPath+"\nand "+crashFile()+
				"\n\nPlease send these files.", walk.MsgBoxIconError)
	}
	if mw := u.win(); mw != nil {
		mw.Synchronize(show)
	}
}
