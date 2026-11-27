//go:build darwin && arm64

package rawconv

// The macOS link line lives in its own file so the Windows and Linux cgo
// inputs (and so their generated symbol names) stay byte-identical to the
// pre-port build.

/*
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../third_party/libraw/lib/darwin_arm64 -lraw -lz -lc++ -lm
*/
import "C"
