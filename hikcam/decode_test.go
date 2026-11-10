package hikcam

import "testing"

func TestUnpackMono8(t *testing.T) {
	src := []byte{0x00, 0x80, 0xFF}
	out, err := unpackMono(src, 3, 1, gvspMono8)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != 0 || out[1] != 0x8080 || out[2] != 0xFFFF {
		t.Fatalf("got %v", out)
	}
}

func TestUnpackMono12(t *testing.T) {
	// little-endian 12-bit values 0x000, 0x800, 0xFFF stored in 16-bit slots
	src := []byte{0x00, 0x00, 0x00, 0x08, 0xFF, 0x0F}
	out, err := unpackMono(src, 3, 1, gvspMono12)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != 0 || out[1] != 0x8000 || out[2] != 0xFFF0 {
		t.Fatalf("got %04x %04x %04x", out[0], out[1], out[2])
	}
}

func TestUnpackMono12Packed(t *testing.T) {
	// pixels 0x123, 0xABC
	// byte0 = 0x12, byte1 = 0xC3, byte2 = 0xAB
	src := []byte{0x12, 0xC3, 0xAB}
	out, err := unpackMono(src, 2, 1, gvspMono12Packed)
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != 0x1230 || out[1] != 0xABC0 {
		t.Fatalf("got %04x %04x", out[0], out[1])
	}
}

func TestUnpackMono10Packed(t *testing.T) {
	// pixels 0x3FF, 0x000, 0x155, 0x2AA
	// high bytes: FF, 00, 55, AA  (pix >> 2)
	// low: 11 00 01 10 = 0xC6?  P0 low=11, P1=00, P2=01, P3=10
	// byte4 bits: P3[1:0] P2[1:0] P1[1:0] P0[1:0] = 10 01 00 11 = 0x93
	src := []byte{0xFF, 0x00, 0x55, 0xAA, 0x93}
	out, err := unpackMono(src, 4, 1, gvspMono10Packed)
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{0x3FF << 6, 0, 0x155 << 6, 0x2AA << 6}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("pix %d: got %04x want %04x", i, out[i], want[i])
		}
	}
}

func TestUnpackRejectsUnknown(t *testing.T) {
	if _, err := unpackMono([]byte{0}, 1, 1, 0xFFFFFFFF); err == nil {
		t.Fatal("expected error")
	}
}

func TestSimGrab(t *testing.T) {
	s := OpenSim()
	if err := s.StartGrab(); err != nil {
		t.Fatal(err)
	}
	f, err := s.Grab(0)
	if err != nil {
		t.Fatal(err)
	}
	if f.W < 16 || f.H < 16 || len(f.Pix) != f.W*f.H {
		t.Fatalf("bad frame %dx%d n=%d", f.W, f.H, len(f.Pix))
	}
	// Default 10 ms should not be all black or all white.
	var min, max uint16 = 65535, 0
	for _, v := range f.Pix {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if min == max {
		t.Fatalf("flat frame min=max=%d", min)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grab(0); err == nil {
		t.Fatal("grab after close should fail")
	}
}

func TestPixelFormatNames(t *testing.T) {
	if PixelAuto.GenICamNames()[0] != "Mono12" {
		t.Fatal("auto should prefer Mono12")
	}
	if PixelMono8.String() != "Mono8" {
		t.Fatal(PixelMono8.String())
	}
}

func TestEnumerateWithoutSDK(t *testing.T) {
	// On this Linux box (and on Windows without MVS) Enumerate must not panic.
	r := Enumerate()
	if r.SDK == "" {
		t.Fatal("empty SDK status")
	}
}
