package hdrctl

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// forkEnv marks a child test process started by forked.
const forkEnv = "NSS_TEST_FORK_CHILD"

// forkSem limits how many forked children run at once. The heavy tests
// are CPU bound under -race (two to three cores each) and one of them
// peaks at about 1.6 GB, so half of GOMAXPROCS keeps the box responsive.
// NSS_TEST_FORKS overrides the limit; GOMAXPROCS=1 (stress.sh) runs them
// one at a time.
var forkSem = func() chan struct{} {
	n := runtime.GOMAXPROCS(0) / 2
	if v, err := strconv.Atoi(os.Getenv("NSS_TEST_FORKS")); err == nil {
		n = v
	}
	if n < 1 {
		n = 1
	}
	return make(chan struct{}, n)
}()

// forked runs the calling test (or subtest) unchanged in a child process
// of the same test binary and reports true in the parent, which then
// returns; in the child it reports false and the test body runs as usual.
// The child has its own globals, environment (isolate's HOME) and temp
// dirs (TMPDIR), so the parent can mark the test parallel even though the body
// uses t.Setenv and package globals. The child is the same -race binary,
// with -count=1 and the parent's remaining timeout; its verbose output is
// logged, a failure fails the test with that output, and a skip skips it.
func forked(t *testing.T) bool {
	t.Helper()
	if os.Getenv(forkEnv) != "" {
		return false
	}
	t.Parallel()
	forkSem <- struct{}{}
	defer func() { <-forkSem }()

	parts := strings.Split(t.Name(), "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	args := []string{"-test.run=" + strings.Join(parts, "/"), "-test.count=1", "-test.v=true"}
	if d, ok := t.Deadline(); ok {
		args = append(args, "-test.timeout="+time.Until(d).Round(time.Second).String())
	}
	if testing.Short() {
		args = append(args, "-test.short")
	}
	cmd := exec.Command(os.Args[0], args...)
	// Its own TMPDIR too: the page writes the meter probes to the fixed
	// os.TempDir()/NegativeScanStudio/probe, which children running at
	// the same time must not share.
	cmd.Env = append(os.Environ(), forkEnv+"="+t.Name(), "TMPDIR="+t.TempDir())
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := out.String()
	switch {
	case err != nil:
		t.Fatalf("forked %s: %v\n%s", t.Name(), err, text)
	case strings.Contains(text, "--- SKIP: "+t.Name()+" "):
		t.Skipf("forked %s skipped:\n%s", t.Name(), text)
	case !strings.Contains(text, "--- PASS: "+t.Name()+" "):
		t.Fatalf("forked %s: no PASS line (test not run?)\n%s", t.Name(), text)
	}
	if testing.Verbose() {
		t.Logf("forked %s:\n%s", t.Name(), text)
	}
	return true
}

// parallelParent marks a test whose subtests call forked as parallel in
// the parent process, so its children overlap with the other forked
// tests. In a child it does nothing: the subtest body may use t.Setenv,
// which panics under a parallel ancestor.
// It reports true in the parent.
func parallelParent(t *testing.T) bool {
	if os.Getenv(forkEnv) != "" {
		return false
	}
	t.Parallel()
	return true
}
