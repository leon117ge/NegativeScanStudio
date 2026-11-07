//go:build !windows

package rawconv

/*
#include <stdlib.h>
#include "libraw/libraw.h"
*/
import "C"

import "unsafe"

func openRaw(lr *C.libraw_data_t, path string) C.int {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	return C.libraw_open_file(lr, cs)
}
