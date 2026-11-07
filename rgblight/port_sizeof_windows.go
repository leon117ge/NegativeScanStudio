//go:build windows

package rgblight

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var unsafeSizeofDCB = unsafe.Sizeof(windows.DCB{})
