package cropper

import (
	"bytes"
	"encoding/binary"
	"sort"
)

const rowsPerStrip = 16

func u16s(v ...uint16) []byte {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], x)
	}
	return b
}

func u32s(v ...uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], x)
	}
	return b
}

// encodeTIFF writes a baseline little-endian RGB TIFF with im.Bits per
// sample, optional LZW + horizontal predictor, plus the copied IFD0 tags
// (ICC profile, make/model, resolution, date, artist, copyright, XMP).
func encodeTIFF(w *bytes.Buffer, im *Image, opt SaveOptions) error {
	w.Write([]byte{'I', 'I', 42, 0, 0, 0, 0, 0})
	pos := uint32(8)
	bps := im.Bits / 8
	rowSamples := im.W * 3
	nStrips := (im.H + rowsPerStrip - 1) / rowsPerStrip
	offsets := make([]uint32, nStrips)
	counts := make([]uint32, nStrips)
	var enc *lzwEncoder
	if opt.LZW {
		enc = newLZWEncoder()
	}
	raw := make([]byte, rowSamples*rowsPerStrip*bps)
	for s := 0; s < nStrips; s++ {
		y0 := s * rowsPerStrip
		y1 := min(y0+rowsPerStrip, im.H)
		src := im.Pix[y0*rowSamples : y1*rowSamples]
		b := raw[:len(src)*bps]
		for r := 0; r < y1-y0; r++ {
			row := src[r*rowSamples : (r+1)*rowSamples]
			if bps == 1 {
				out := b[r*rowSamples : (r+1)*rowSamples]
				prev := [3]uint8{}
				for i, v := range row {
					q := to8(v)
					if opt.LZW {
						out[i] = q - prev[i%3]
						prev[i%3] = q
					} else {
						out[i] = q
					}
				}
			} else {
				out := b[r*rowSamples*2 : (r+1)*rowSamples*2]
				prev := [3]uint16{}
				for i, v := range row {
					q := v
					if opt.LZW {
						q = v - prev[i%3]
						prev[i%3] = v
					}
					binary.LittleEndian.PutUint16(out[i*2:], q)
				}
			}
		}
		data := b
		if opt.LZW {
			data = enc.Encode(b)
		}
		offsets[s] = pos
		counts[s] = uint32(len(data))
		w.Write(data)
		pos += uint32(len(data))
	}
	comp := uint16(1)
	if opt.LZW {
		comp = 5
	}
	bits := uint16(im.Bits)
	entries := []rawTag{
		{256, 4, 1, u32s(uint32(im.W))},
		{257, 4, 1, u32s(uint32(im.H))},
		{258, 3, 3, u16s(bits, bits, bits)},
		{259, 3, 1, u16s(comp)},
		{262, 3, 1, u16s(2)},
		{273, 4, uint32(nStrips), u32s(offsets...)},
		{277, 3, 1, u16s(3)},
		{278, 4, 1, u32s(rowsPerStrip)},
		{279, 4, uint32(nStrips), u32s(counts...)},
		{284, 3, 1, u16s(1)},
	}
	if opt.LZW {
		entries = append(entries, rawTag{317, 3, 1, u16s(2)})
	}
	have := map[uint16]bool{}
	for _, e := range entries {
		have[e.id] = true
	}
	for _, t := range im.meta.tiffTags {
		if have[t.id] || (t.id == 305 && opt.Software != "") {
			continue
		}
		entries = append(entries, t)
		have[t.id] = true
	}
	if opt.Software != "" {
		entries = append(entries, rawTag{305, 2, uint32(len(opt.Software) + 1), append([]byte(opt.Software), 0)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
	if pos%2 == 1 {
		w.WriteByte(0)
		pos++
	}
	ifdOff := pos
	ifdSize := uint32(2 + 12*len(entries) + 4)
	extra := ifdOff + ifdSize
	var ext []byte
	ifd := u16s(uint16(len(entries)))
	for _, e := range entries {
		ifd = append(ifd, u16s(e.id, e.typ)...)
		ifd = append(ifd, u32s(e.count)...)
		if len(e.data) <= 4 {
			v := make([]byte, 4)
			copy(v, e.data)
			ifd = append(ifd, v...)
		} else {
			ifd = append(ifd, u32s(extra+uint32(len(ext)))...)
			ext = append(ext, e.data...)
			if len(ext)%2 == 1 {
				ext = append(ext, 0)
			}
		}
	}
	ifd = append(ifd, 0, 0, 0, 0)
	w.Write(ifd)
	w.Write(ext)
	binary.LittleEndian.PutUint32(w.Bytes()[4:], ifdOff)
	return nil
}
