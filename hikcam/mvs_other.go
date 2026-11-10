//go:build !windows

package hikcam

// SDKAvailable is false on this OS: the MVS runtime is a Windows DLL.
func SDKAvailable() bool { return false }

// SDKStatus explains why real cameras cannot be opened here.
func SDKStatus() string {
	return "Hikrobot MVS SDK (MvCameraControl.dll) is a Windows runtime; this build cannot load it"
}

// Enumerate returns no real cameras and ErrNoSDK on non-Windows builds.
func Enumerate() ListResult {
	return ListResult{SDK: SDKStatus(), Err: ErrNoSDK}
}

// OpenReal always fails on non-Windows builds.
func OpenReal(Info) (Device, error) { return nil, ErrNoSDK }
