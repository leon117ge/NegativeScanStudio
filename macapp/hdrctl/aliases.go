package hdrctl

import "basetool/macapp/appstate"

// Names the copied Windows declarations (copied.go) refer to.
var (
	isoVals          = appstate.ISOVals
	targetVals       = appstate.TargetVals
	fixedShutterVals = appstate.FixedShutterVals
)

const appVersion = appstate.AppVersion

func configDir() string { return appstate.ConfigDir() }

func clampIdx(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

func fixedShutterIndexOf(us uint32) int { return appstate.FixedShutterIndexOf(us) }
