//go:build darwin && cgo

package camera

import "testing"

// macOS has no ImageCaptureCore authorization API: the helper must report
// authorized for both, return no error, and not need a camera or the run loop.
func TestICCAuthorizationMacOS(t *testing.T) {
	ctl, cnt, err := ICCAuthorization(0)
	if err != nil {
		t.Fatalf("ICCAuthorization: %v", err)
	}
	if ctl != ICCAuthorizedMacOS || cnt != ICCAuthorizedMacOS {
		t.Fatalf("ICCAuthorization = %q, %q", ctl, cnt)
	}
}
