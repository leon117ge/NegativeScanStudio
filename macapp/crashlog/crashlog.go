// Package crashlog writes the macOS app's crash log to
// ~/Library/Logs/NegativeScanStudio/crash_log.txt (the Windows app keeps
// crash_log.txt in its settings folder; Console.app shows ~/Library/Logs).
package crashlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// Dir is ~/Library/Logs/NegativeScanStudio (NSS_LOG_DIR overrides it, for tests).
func Dir() string {
	if d := os.Getenv("NSS_LOG_DIR"); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "NegativeScanStudio")
	}
	return filepath.Join(h, "Library", "Logs", "NegativeScanStudio")
}

// File is the crash log.
func File() string { return filepath.Join(Dir(), "crash_log.txt") }

var (
	mu      sync.Mutex
	version = "NegativeScan Studio"
)

// Append adds one entry.
func Append(text string) {
	mu.Lock()
	defer mu.Unlock()
	_ = os.MkdirAll(Dir(), 0o755)
	f, err := os.OpenFile(File(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "==== %s %s (macOS) ====\n%s\n", time.Now().Format("2006-01-02 15:04:05.000"), version, text)
}

// Install routes fatal runtime errors and unrecovered panics to the crash
// log (append mode: the file is only written when something dies).
func Install(appVersion string) {
	version = appVersion
	_ = os.MkdirAll(Dir(), 0o755)
	f, err := os.OpenFile(File(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	f.Close()
}

// Guard is deferred around UI callbacks and jobs: it records a panic with
// its stack and returns true so the caller can carry on. Use as
//
//	defer crashlog.Guard("where", nil)
func Guard(where string, onPanic func(r any)) {
	if r := recover(); r != nil {
		Append(fmt.Sprintf("panic in %s: %v\n%s", where, r, debug.Stack()))
		if onPanic != nil {
			onPanic(r)
		}
	}
}

// Main is deferred in main: it records a panic that reached the top and
// re-raises it.
func Main() {
	if r := recover(); r != nil {
		Append(fmt.Sprintf("unrecovered panic in main: %v\n%s", r, debug.Stack()))
		panic(r)
	}
}
