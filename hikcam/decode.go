package hikcam

import "fmt"

// GigE Vision / USB3 Vision pixel type IDs used by Hikrobot MVS.
const (
	gvspMono8        = 0x01080001
	gvspMono10       = 0x01100003
	gvspMono10Packed = 0x010C0004
	gvspMono12       = 0x01100005
	gvspMono12Packed = 0x010C0006
	gvspMono16       = 0x01100007
)

func pixelTypeName(t uint32) string {
	switch t {
	case gvspMono8:
		return "Mono8"
	case gvspMono10:
		return "Mono10"
	case gvspMono10Packed:
		return "Mono10Packed"
	case gvspMono12:
		return "Mono12"
	case gvspMono12Packed:
		return "Mono12Packed"
	case gvspMono16:
		return "Mono16"
	default:
		return fmt.Sprintf("0x%08X", t)
	}
}

// unpackMono expands a native Mono8/10/12/16 buffer into 16-bit linear
// samples (left-justified in the 16-bit range).
func unpackMono(src []byte, w, h int, pixType uint32) ([]uint16, error) {
	n := w * h
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid size %dx%d", w, h)
	}
	out := make([]uint16, n)
	switch pixType {
	case gvspMono8:
		if len(src) < n {
			return nil, fmt.Errorf("Mono8 buffer too short: %d < %d", len(src), n)
		}
		for i := 0; i < n; i++ {
			v := uint16(src[i])
			out[i] = v<<8 | v
		}
	case gvspMono10:
		if len(src) < 2*n {
			return nil, fmt.Errorf("Mono10 buffer too short: %d < %d", len(src), 2*n)
		}
		for i := 0; i < n; i++ {
			v := uint16(src[2*i]) | uint16(src[2*i+1])<<8
			out[i] = (v & 0x03FF) << 6
		}
	case gvspMono12:
		if len(src) < 2*n {
			return nil, fmt.Errorf("Mono12 buffer too short: %d < %d", len(src), 2*n)
		}
		for i := 0; i < n; i++ {
			v := uint16(src[2*i]) | uint16(src[2*i+1])<<8
			out[i] = (v & 0x0FFF) << 4
		}
	case gvspMono16:
		if len(src) < 2*n {
			return nil, fmt.Errorf("Mono16 buffer too short: %d < %d", len(src), 2*n)
		}
		for i := 0; i < n; i++ {
			out[i] = uint16(src[2*i]) | uint16(src[2*i+1])<<8
		}
	case gvspMono10Packed:
		need := (n + 3) / 4 * 5
		if len(src) < need {
			return nil, fmt.Errorf("Mono10Packed buffer too short: %d < %d", len(src), need)
		}
		if err := unpackMono10Packed(src, out); err != nil {
			return nil, err
		}
	case gvspMono12Packed:
		need := (n + 1) / 2 * 3
		if len(src) < need {
			return nil, fmt.Errorf("Mono12Packed buffer too short: %d < %d", len(src), need)
		}
		if err := unpackMono12Packed(src, out); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrBadFormat, pixelTypeName(pixType))
	}
	return out, nil
}

// unpackMono12Packed decodes GigE Vision Mono12 Packed (2 pixels / 3 bytes)
// and left-justifies into 16-bit.
func unpackMono12Packed(src []byte, out []uint16) error {
	n := len(out)
	si := 0
	for i := 0; i+1 < n; i += 2 {
		if si+2 >= len(src) {
			return fmt.Errorf("Mono12Packed truncated at pixel %d", i)
		}
		a := uint16(src[si])<<4 | uint16(src[si+1]&0x0F)
		b := uint16(src[si+2])<<4 | uint16(src[si+1]>>4)
		out[i] = (a & 0x0FFF) << 4
		out[i+1] = (b & 0x0FFF) << 4
		si += 3
	}
	if n%2 == 1 {
		if si+1 >= len(src) {
			return fmt.Errorf("Mono12Packed truncated at last pixel")
		}
		a := uint16(src[si])<<4 | uint16(src[si+1]&0x0F)
		out[n-1] = (a & 0x0FFF) << 4
	}
	return nil
}

// unpackMono10Packed decodes GigE Vision Mono10 Packed (4 pixels / 5 bytes).
func unpackMono10Packed(src []byte, out []uint16) error {
	n := len(out)
	si := 0
	i := 0
	for i+3 < n {
		if si+4 >= len(src) {
			return fmt.Errorf("Mono10Packed truncated at pixel %d", i)
		}
		low := src[si+4]
		out[i] = (uint16(src[si])<<2 | uint16(low&0x03)) << 6
		out[i+1] = (uint16(src[si+1])<<2 | uint16((low>>2)&0x03)) << 6
		out[i+2] = (uint16(src[si+2])<<2 | uint16((low>>4)&0x03)) << 6
		out[i+3] = (uint16(src[si+3])<<2 | uint16((low>>6)&0x03)) << 6
		si += 5
		i += 4
	}
	for k := 0; i < n; k++ {
		if si+k >= len(src) {
			return fmt.Errorf("Mono10Packed truncated at pixel %d", i)
		}
		low := byte(0)
		if si+4 < len(src) {
			low = src[si+4]
		}
		out[i] = (uint16(src[si+k])<<2 | uint16((low>>(2*k))&0x03)) << 6
		i++
	}
	return nil
}

// gray8 returns an 8-bit preview of a 16-bit frame (high byte).
func gray8(pix []uint16) []byte {
	out := make([]byte, len(pix))
	for i, v := range pix {
		out[i] = byte(v >> 8)
	}
	return out
}
