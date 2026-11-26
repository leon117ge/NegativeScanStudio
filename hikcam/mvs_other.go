//go:build !windows && !(darwin && cgo)

package hikcam

// SDKAvailable is false in this build: the MVS loader exists only for
// Windows (MvCameraControl.dll) and macOS with cgo (MvCameraControl.framework).
func SDKAvailable() bool { return false }

// SDKStatus explains why real cameras cannot be opened here.
func SDKStatus() string {
	return "Hikrobot MVS SDK loading is only built for Windows and for macOS with cgo; this build cannot load it"
}

// Enumerate returns no real cameras and ErrNoSDK in this build.
func Enumerate() ListResult {
	return ListResult{SDK: SDKStatus(), Err: ErrNoSDK}
}

// OpenReal always fails in this build.
func OpenReal(Info) (Device, error) { return nil, ErrNoSDK }
