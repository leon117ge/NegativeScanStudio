package rawconv

import "testing"

// Every platform links the same LibRaw release (windows_amd64 and
// linux_amd64 vendored, darwin_arm64 built by ci/macos/build_libraw.sh).
func TestLibRawVersion(t *testing.T) {
	if v := LibRawVersion(); v != "0.22.1-Release" {
		t.Fatalf("LibRaw %s, want 0.22.1-Release", v)
	}
}
