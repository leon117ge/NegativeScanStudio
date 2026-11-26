package crashlog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirIsLibraryLogs(t *testing.T) {
	t.Setenv("NSS_LOG_DIR", "")
	h, _ := os.UserHomeDir()
	want := filepath.Join(h, "Library", "Logs", "NegativeScanStudio")
	if Dir() != want {
		t.Fatalf("Dir() = %q, want %q", Dir(), want)
	}
	if runtime.GOOS == "darwin" && !strings.HasSuffix(File(), "/Library/Logs/NegativeScanStudio/crash_log.txt") {
		t.Fatalf("File() = %q", File())
	}
}

func TestGuardWritesLogAndRecovers(t *testing.T) {
	d := t.TempDir()
	t.Setenv("NSS_LOG_DIR", d)
	got := false
	func() {
		defer Guard("test job", func(any) { got = true })
		panic("boom")
	}()
	if !got {
		t.Fatal("onPanic not called")
	}
	b, err := os.ReadFile(filepath.Join(d, "crash_log.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "panic in test job: boom") || !strings.Contains(s, "goroutine") {
		t.Fatalf("crash log:\n%s", s)
	}
}
