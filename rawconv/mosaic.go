package rawconv

/*
#include <stdlib.h>
#include <string.h>
#include "libraw/libraw.h"

// Same parameters as fbm_set_params in libraw.go (keep in sync).
static void fbm_set_params_m(libraw_data_t *lr) {
	libraw_output_params_t *p = &lr->params;
	p->user_mul[0] = 1.0f; p->user_mul[1] = 1.0f; p->user_mul[2] = 1.0f; p->user_mul[3] = 1.0f;
	p->use_camera_wb = 0;
	p->use_auto_wb = 0;
	p->output_color = 0;
	p->user_flip = 0;
	p->highlight = 1;
	p->output_bps = 16;
	p->no_auto_bright = 1;
	p->user_qual = 3;
	p->gamm[0] = 1.0;
	p->gamm[1] = 1.0;
	p->adjust_maximum_thr = 0.0f;
	p->use_camera_matrix = 0;
}

typedef struct {
	libraw_processed_image_t *img;
	int err;
	int stage;
	int rw, rh;
	unsigned maxv;
} fbm_mres;

// Unpack the file, replace its sensor data with mos (raw_width x raw_height,
// no padding) and its white level with maxv, then demosaic as usual.
static fbm_mres fbm_process_mosaic(libraw_data_t *lr, const unsigned short *mos, int rw, int rh, unsigned maxv) {
	fbm_mres r;
	memset(&r, 0, sizeof(r));
	r.stage = 2;
	int rc = libraw_unpack(lr);
	if (rc != LIBRAW_SUCCESS) { r.err = rc; return r; }
	if (!lr->rawdata.raw_image) { r.err = -100; return r; }
	r.rw = lr->sizes.raw_width; r.rh = lr->sizes.raw_height;
	if (r.rw != rw || r.rh != rh) { r.err = -101; return r; }
	int pitch = lr->sizes.raw_pitch / 2;
	for (int y = 0; y < rh; y++)
		memcpy(lr->rawdata.raw_image + (size_t)y * pitch, mos + (size_t)y * rw, (size_t)rw * 2);
	lr->color.maximum = maxv;
	lr->rawdata.color.maximum = maxv;
	r.maxv = lr->color.maximum;
	r.stage = 3;
	rc = libraw_dcraw_process(lr);
	if (rc != LIBRAW_SUCCESS) { r.err = rc; return r; }
	r.stage = 4;
	int errc = 0;
	r.img = libraw_dcraw_make_mem_image(lr, &errc);
	if (!r.img) { r.err = errc ? errc : -1; return r; }
	return r;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// DecodeMosaic demosaics replacement sensor data through the same LibRaw
// pipeline as Decode. path supplies the metadata (CFA, black levels,
// geometry); raw is RawWidth*RawHeight sensor values on the file's black
// level and maximum is the white level that maps to 65535 (raise it to keep
// extra precision, e.g. black + 4*(white-black) with the signal scaled by 4).
// Call Free on the result.
func DecodeMosaic(path string, raw []uint16, rawWidth, rawHeight, maximum int) (*Image, error) {
	if len(raw) != rawWidth*rawHeight || len(raw) == 0 {
		return nil, errors.New("mosaic size mismatch")
	}
	lr := C.libraw_init(0)
	if lr == nil {
		return nil, errors.New("libraw_init failed")
	}
	defer C.libraw_close(lr)
	C.fbm_set_params_m(lr)
	if rc := openRaw(lr, path); rc != 0 {
		return nil, fmt.Errorf("cannot open RAW: %s", librawErr(rc))
	}
	r := C.fbm_process_mosaic(lr, (*C.ushort)(unsafe.Pointer(&raw[0])), C.int(rawWidth), C.int(rawHeight), C.uint(maximum))
	if r.err != 0 || r.img == nil {
		switch r.err {
		case -100:
			return nil, errors.New("not a CFA raw (no raw_image)")
		case -101:
			return nil, fmt.Errorf("sensor size %dx%d differs from the mosaic %dx%d", int(r.rw), int(r.rh), rawWidth, rawHeight)
		}
		stage := map[int]string{2: "unpack", 3: "process", 4: "make image"}[int(r.stage)]
		return nil, fmt.Errorf("LibRaw %s failed: %s", stage, librawErr(r.err))
	}
	img := r.img
	if img._type != C.LIBRAW_IMAGE_BITMAP || img.colors != 3 || img.bits != 16 {
		C.libraw_dcraw_clear_mem(img)
		return nil, fmt.Errorf("unexpected LibRaw output (type %d, %d colours, %d bits)", int(img._type), int(img.colors), int(img.bits))
	}
	w, h := int(img.width), int(img.height)
	dataPtr := unsafe.Pointer(uintptr(unsafe.Pointer(img)) + unsafe.Offsetof(img.data))
	return &Image{Width: w, Height: h, Pix: unsafe.Slice((*uint16)(dataPtr), w*h*3), img: img}, nil
}
