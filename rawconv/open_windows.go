package rawconv

/*
#include "libraw/libraw.h"
*/
import "C"

import (
	"syscall"
	"unsafe"
)

// openRaw uses the wide-char API so non-ASCII Windows paths work.
func openRaw(lr *C.libraw_data_t, path string) C.int {
	u, err := syscall.UTF16FromString(path)
	if err != nil {
		return C.LIBRAW_IO_ERROR
	}
	return C.libraw_open_wfile(lr, (*C.wchar_t)(unsafe.Pointer(&u[0])))
}
