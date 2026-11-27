package rawconv

/*
#cgo CFLAGS: -I${SRCDIR}/../third_party/libraw/include -DLIBRAW_NODLL
#cgo windows LDFLAGS: -L${SRCDIR}/../third_party/libraw/lib/windows_amd64 -lraw -lz -lws2_32 -static -lstdc++ -lm
#cgo linux LDFLAGS: -L${SRCDIR}/../third_party/libraw/lib/linux_amd64 -lraw -lz -lstdc++ -lm
#include <stdlib.h>
#include <string.h>
#include "libraw/libraw.h"

// Same LibRaw settings as open_make_tiff (decodeDirect / decodeAdobeDNG):
// linear 16-bit, camera native RGB, no white balance, no auto bright.
static void fbm_set_params(libraw_data_t *lr) {
	libraw_output_params_t *p = &lr->params;
	p->user_mul[0] = 1.0f; p->user_mul[1] = 1.0f; p->user_mul[2] = 1.0f; p->user_mul[3] = 1.0f;
	p->use_camera_wb = 0;
	p->use_auto_wb = 0;
	p->output_color = 0;      // raw camera colour, no matrix
	p->user_flip = 0;         // no rotation
	p->highlight = 1;         // unclip
	p->output_bps = 16;
	p->no_auto_bright = 1;
	p->user_qual = 3;         // AHD (X-Trans uses xtrans_interpolate)
	p->gamm[0] = 1.0;         // gamma 1.0
	p->gamm[1] = 1.0;         // toe slope 1.0
	p->adjust_maximum_thr = 0.0f;
	p->use_camera_matrix = 0;
}

typedef struct {
	libraw_processed_image_t *img;
	float cam_mul[4];
	float cam_xyz[4][3];
	char make[64];
	char model[64];
	int err;
	int stage;
} fbm_result;

static fbm_result fbm_process(libraw_data_t *lr, int inset_crop) {
	fbm_result r;
	memset(&r, 0, sizeof(r));
	int rc;
	if (inset_crop) {
		libraw_adjust_to_raw_inset_crop(lr, 3, 0.0f);
	}
	r.stage = 2;
	rc = libraw_unpack(lr);
	if (rc != LIBRAW_SUCCESS) { r.err = rc; return r; }
	r.stage = 3;
	rc = libraw_dcraw_process(lr);
	if (rc != LIBRAW_SUCCESS) { r.err = rc; return r; }
	r.stage = 4;
	int errc = 0;
	r.img = libraw_dcraw_make_mem_image(lr, &errc);
	if (!r.img) { r.err = errc ? errc : -1; return r; }
	for (int i = 0; i < 4; i++) r.cam_mul[i] = lr->color.cam_mul[i];
	for (int i = 0; i < 4; i++)
		for (int j = 0; j < 3; j++)
			r.cam_xyz[i][j] = lr->color.cam_xyz[i][j];
	strncpy(r.make, lr->idata.make, 63);
	strncpy(r.model, lr->idata.model, 63);
	return r;
}

// Photograph path: as-shot WB + camera matrix into linear sRGB.
static void fbm_set_params_scene(libraw_data_t *lr) {
	fbm_set_params(lr);
	libraw_output_params_t *p = &lr->params;
	p->use_camera_wb = 1;
	p->use_auto_wb = 0;
	p->output_color = 1;
	p->use_camera_matrix = 1;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Image is a decoded linear 16-bit RGB image. Pix points to LibRaw-owned memory
// and is only valid until Free is called.
type Image struct {
	Width, Height int
	Pix           []uint16 // RGBRGB..., native (little) endian
	CamMul        [4]float32
	CamXYZ        [4][3]float32
	Make, Model   string
	img           *C.libraw_processed_image_t
}

func (im *Image) Free() {
	if im != nil && im.img != nil {
		C.libraw_dcraw_clear_mem(im.img)
		im.img = nil
		im.Pix = nil
	}
}

func librawErr(code C.int) string {
	return C.GoString(C.libraw_strerror(code))
}

// decode opens path with LibRaw and returns the processed linear image.
func decode(path string, insetCrop bool) (*Image, error) {
	return decodeMode(path, insetCrop, false)
}

func decodeMode(path string, insetCrop, scene bool) (*Image, error) {
	lr := C.libraw_init(0)
	if lr == nil {
		return nil, errors.New("libraw_init failed")
	}
	defer C.libraw_close(lr)
	if scene {
		C.fbm_set_params_scene(lr)
	} else {
		C.fbm_set_params(lr)
	}
	if rc := openRaw(lr, path); rc != 0 {
		return nil, fmt.Errorf("cannot open RAW: %s", librawErr(rc))
	}
	ic := C.int(0)
	if insetCrop {
		ic = 1
	}
	r := C.fbm_process(lr, ic)
	if r.err != 0 || r.img == nil {
		stage := map[int]string{2: "unpack", 3: "process", 4: "make image"}[int(r.stage)]
		return nil, fmt.Errorf("LibRaw %s failed: %s", stage, librawErr(r.err))
	}
	img := r.img
	if img._type != C.LIBRAW_IMAGE_BITMAP || img.colors != 3 || img.bits != 16 {
		C.libraw_dcraw_clear_mem(img)
		return nil, fmt.Errorf("unexpected LibRaw output (type %d, %d colours, %d bits)", int(img._type), int(img.colors), int(img.bits))
	}
	w, h := int(img.width), int(img.height)
	n := w * h * 3
	dataPtr := unsafe.Pointer(uintptr(unsafe.Pointer(img)) + unsafe.Offsetof(img.data))
	out := &Image{
		Width:  w,
		Height: h,
		Pix:    unsafe.Slice((*uint16)(dataPtr), n),
		Make:   C.GoString(&r.make[0]),
		Model:  C.GoString(&r.model[0]),
		img:    img,
	}
	for i := 0; i < 4; i++ {
		out.CamMul[i] = float32(r.cam_mul[i])
	}
	for i := 0; i < 4; i++ {
		for j := 0; j < 3; j++ {
			out.CamXYZ[i][j] = float32(r.cam_xyz[i][j])
		}
	}
	return out, nil
}

// LibRawVersion returns the linked LibRaw version string.
func LibRawVersion() string { return C.GoString(C.libraw_version()) }
