package rawconv

/*
#include <stdlib.h>
#include <string.h>
#include "libraw/libraw.h"

typedef struct {
	int raw_width, raw_height, raw_pitch;
	int width, height, top, left;
	unsigned black, maximum;
	unsigned cblack[4+2+36];
	unsigned short *raw_image;
	char make[64], model[64];
	float iso, shutter, aperture;
	int err;
} fbm_rawinfo;

static fbm_rawinfo fbm_unpack_info(libraw_data_t *lr) {
	fbm_rawinfo r;
	memset(&r, 0, sizeof(r));
	int rc = libraw_unpack(lr);
	if (rc != LIBRAW_SUCCESS) { r.err = rc; return r; }
	if (!lr->rawdata.raw_image) { r.err = -100; return r; }
	r.raw_width = lr->sizes.raw_width;
	r.raw_height = lr->sizes.raw_height;
	r.raw_pitch = lr->sizes.raw_pitch;
	r.width = lr->sizes.width;
	r.height = lr->sizes.height;
	r.top = lr->sizes.top_margin;
	r.left = lr->sizes.left_margin;
	r.black = lr->color.black;
	r.maximum = lr->color.maximum;
	for (int i = 0; i < 42; i++) r.cblack[i] = lr->color.cblack[i];
	r.raw_image = lr->rawdata.raw_image;
	strncpy(r.make, lr->idata.make, 63);
	strncpy(r.model, lr->idata.model, 63);
	r.iso = lr->other.iso_speed;
	r.shutter = lr->other.shutter;
	r.aperture = lr->other.aperture;
	return r;
}
static void fbm_exposure(libraw_data_t *lr, float *iso, float *shutter) {
	*iso = lr->other.iso_speed;
	*shutter = lr->other.shutter;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// RawFrame is the undemosaiced sensor data of a RAW file plus what is needed
// to put it on the same linear scale as the TIFF output:
// tiff ≈ (raw - BlackAt) * 65535 / (Maximum - Black).
type RawFrame struct {
	RawWidth, RawHeight int // full sensor incl. margins
	Width, Height       int // visible area
	Top, Left           int
	Black, Maximum      int
	BlackAt             []uint16 // per visible pixel total black level (Width*Height)
	Raw                 []uint16 // RawWidth*RawHeight
	Color               []uint8  // Width*Height CFA colour 0..3 (3 = second green)
	Make, Model         string
	ISO, Shutter, FNum  float64
}

// ReadExposure returns a RAW file's EXIF ISO and exposure time (seconds)
// without unpacking the sensor data.
func ReadExposure(path string) (iso, shutter float64, err error) {
	lr := C.libraw_init(0)
	if lr == nil {
		return 0, 0, errors.New("libraw_init failed")
	}
	defer C.libraw_close(lr)
	if rc := openRaw(lr, path); rc != 0 {
		return 0, 0, fmt.Errorf("cannot open RAW: %s", librawErr(rc))
	}
	var i, s C.float
	C.fbm_exposure(lr, &i, &s)
	return float64(i), float64(s), nil
}

// ReadRawFrame unpacks a RAW file without demosaicing (fast).
func ReadRawFrame(path string) (*RawFrame, error) {
	lr := C.libraw_init(0)
	if lr == nil {
		return nil, errors.New("libraw_init failed")
	}
	defer C.libraw_close(lr)
	if rc := openRaw(lr, path); rc != 0 {
		return nil, fmt.Errorf("cannot open RAW: %s", librawErr(rc))
	}
	r := C.fbm_unpack_info(lr)
	if r.err != 0 {
		if r.err == -100 {
			return nil, errors.New("not a CFA raw (no raw_image)")
		}
		return nil, fmt.Errorf("LibRaw unpack failed: %s", librawErr(r.err))
	}
	rw, rh := int(r.raw_width), int(r.raw_height)
	pitch := int(r.raw_pitch) / 2
	f := &RawFrame{
		RawWidth: rw, RawHeight: rh, Width: int(r.width), Height: int(r.height),
		Top: int(r.top), Left: int(r.left), Black: int(r.black), Maximum: int(r.maximum),
		Make: C.GoString(&r.make[0]), Model: C.GoString(&r.model[0]),
		ISO: float64(r.iso), Shutter: float64(r.shutter), FNum: float64(r.aperture),
	}
	var cb [42]int
	for i := 0; i < 42; i++ {
		cb[i] = int(r.cblack[i])
	}
	src := unsafe.Slice((*uint16)(unsafe.Pointer(r.raw_image)), pitch*rh)
	f.Raw = make([]uint16, rw*rh)
	for y := 0; y < rh; y++ {
		copy(f.Raw[y*rw:(y+1)*rw], src[y*pitch:y*pitch+rw])
	}
	f.Color = make([]uint8, f.Width*f.Height)
	// CFA pattern repeats with period ≤ 6 (X-Trans) or 2/16 (Bayer); sample it once per 48x48 tile.
	const P = 48
	var pat [P][P]uint8
	for y := 0; y < P; y++ {
		for x := 0; x < P; x++ {
			pat[y][x] = uint8(C.libraw_COLOR(lr, C.int(y), C.int(x)))
		}
	}
	f.BlackAt = make([]uint16, f.Width*f.Height)
	for y := 0; y < f.Height; y++ {
		row := f.Color[y*f.Width : (y+1)*f.Width]
		brow := f.BlackAt[y*f.Width : (y+1)*f.Width]
		py := &pat[y%P]
		for x := range row {
			c := py[x%P]
			row[x] = c
			b := f.Black + cb[c]
			if cb[4] > 0 && cb[5] > 0 && cb[4]*cb[5] <= 36 {
				// LibRaw indexes the pattern by raw-image position.
				b += cb[6+((y+f.Top)%cb[4])*cb[5]+(x+f.Left)%cb[5]]
			}
			brow[x] = uint16(b)
		}
	}
	// TIFF scale uses maximum minus the common black (LibRaw subtracts it).
	mb := 0
	if len(f.BlackAt) > 0 {
		mb = int(f.BlackAt[0])
		for _, b := range f.BlackAt[:min(len(f.BlackAt), 4096)] {
			mb = min(mb, int(b))
		}
	}
	f.Black = mb
	return f, nil
}

// Binned returns a small linear RGB image (0..1 of TIFF full scale) made by
// averaging each bin×bin block of sensor pixels per colour. For flat regions
// it matches the demosaiced TIFF; it is used for metering and previews.
// clipped[i] is set when any sensor pixel in the block reaches the white level.
func (f *RawFrame) Binned(bin int) (w, h int, rgb []float32, clipped []bool) {
	w, h = f.Width/bin, f.Height/bin
	rgb = make([]float32, w*h*3)
	clipped = make([]bool, w*h)
	scale := 1.0 / float64(f.Maximum-f.Black)
	clipAt := uint16(f.Maximum)
	if f.Maximum > 64 {
		clipAt = uint16(f.Maximum - 8)
	}
	for by := 0; by < h; by++ {
		for bx := 0; bx < w; bx++ {
			var sum [4]float64
			var n [4]int
			clip := false
			for yy := 0; yy < bin; yy++ {
				y := by*bin + yy
				rrow := f.Raw[(y+f.Top)*f.RawWidth+f.Left:]
				crow := f.Color[y*f.Width:]
				blrow := f.BlackAt[y*f.Width:]
				for xx := 0; xx < bin; xx++ {
					x := bx*bin + xx
					c := crow[x]
					v := rrow[x]
					if v >= clipAt {
						clip = true
					}
					sum[c] += float64(int(v) - int(blrow[x]))
					n[c]++
				}
			}
			g := 0.0
			gn := 0
			if n[1] > 0 {
				g += sum[1]
				gn += n[1]
			}
			if n[3] > 0 {
				g += sum[3]
				gn += n[3]
			}
			i := (by*w + bx)
			if n[0] > 0 {
				rgb[i*3] = float32(sum[0] / float64(n[0]) * scale)
			}
			if gn > 0 {
				rgb[i*3+1] = float32(g / float64(gn) * scale)
			}
			if n[2] > 0 {
				rgb[i*3+2] = float32(sum[2] / float64(n[2]) * scale)
			}
			clipped[i] = clip
		}
	}
	return
}

// Decode runs the full linear LibRaw pipeline (same as Convert). Call Free.
func Decode(path string) (*Image, error) { return decode(path, false) }

// WriteTIFF16 writes a linear 16-bit RGB TIFF with the converter's writer.
func WriteTIFF16(path string, w, h int, pix []uint16, lzw bool, make_, model, software string) error {
	return writeTIFF(path, w, h, pix, lzw, tiffMeta{Make: make_, Model: model, Software: software})
}
