package rawconv

import (
	"math"
	"testing"
)

func TestChannelPlane(t *testing.T) {
	// 8x8 RGGB frame, 1 px margin; R=1024, G=2048(+G2), B=4096 above black 512
	f := &RawFrame{RawWidth: 10, RawHeight: 10, Width: 8, Height: 8, Top: 1, Left: 1, Black: 512, Maximum: 16383 + 512}
	f.Raw = make([]uint16, 100)
	f.Color = make([]uint8, 64)
	f.BlackAt = make([]uint16, 64)
	val := [4]uint16{512 + 1024, 512 + 2048, 512 + 4096, 512 + 2048}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := [4]uint8{0, 1, 3, 2}[(y%2)*2+x%2]
			f.Color[y*8+x] = c
			f.BlackAt[y*8+x] = 512
			f.Raw[(y+1)*10+x+1] = val[c]
		}
	}
	f.Raw[1*10+1] = uint16(f.Maximum) // clip one R photosite
	for ch, want := range []float64{1024, 2048, 4096} {
		w, h, v, cl := f.ChannelPlane(ch, 4)
		if w != 2 || h != 2 {
			t.Fatalf("size %dx%d", w, h)
		}
		if ch != 0 && math.Abs(float64(v[3])-want/16383) > 1e-6 {
			t.Fatalf("ch %d = %v want %v", ch, v[3], want/16383)
		}
		if (ch == 0) != cl[0] || cl[3] {
			t.Fatalf("ch %d clip %v", ch, cl)
		}
	}
}
