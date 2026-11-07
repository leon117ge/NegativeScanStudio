package rawconv

import (
	"bufio"
	"encoding/binary"
	"os"
	"sort"
	"unsafe"
)

const rowsPerStrip = 16

type tiffMeta struct {
	Make, Model, Software string
	ICC                   []byte
	Description           string
}

type ifdEntry struct {
	tag, typ uint16
	count    uint32
	data     []byte // raw little-endian value bytes
}

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
func ascii(s string) []byte { return append([]byte(s), 0) }

// writeTIFF writes a baseline 16-bit RGB (chunky) little-endian TIFF.
// compress=true uses LZW with horizontal predictor (Predictor=2).
func writeTIFF(path string, w, h int, pix []uint16, compress bool, meta tiffMeta) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(path)
		}
	}()
	bw := bufio.NewWriterSize(f, 1<<20)
	// header, IFD offset patched later
	if _, err := bw.Write([]byte{'I', 'I', 42, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	pos := uint32(8)
	rowSamples := w * 3
	rowBytes := rowSamples * 2
	nStrips := (h + rowsPerStrip - 1) / rowsPerStrip
	offsets := make([]uint32, nStrips)
	counts := make([]uint32, nStrips)

	var enc *lzwEncoder
	var tmp []uint16
	if compress {
		enc = newLZWEncoder()
		tmp = make([]uint16, rowSamples*rowsPerStrip)
	}
	for s := 0; s < nStrips; s++ {
		y0 := s * rowsPerStrip
		y1 := y0 + rowsPerStrip
		if y1 > h {
			y1 = h
		}
		src := pix[y0*rowSamples : y1*rowSamples]
		var data []byte
		if compress {
			t := tmp[:len(src)]
			for r := 0; r < y1-y0; r++ {
				row := src[r*rowSamples : (r+1)*rowSamples]
				out := t[r*rowSamples : (r+1)*rowSamples]
				copy(out[:3], row[:3])
				for i := 3; i < rowSamples; i++ {
					out[i] = row[i] - row[i-3]
				}
			}
			data = enc.Encode(u16bytes(t))
		} else {
			data = u16bytes(src)
		}
		offsets[s] = pos
		counts[s] = uint32(len(data))
		if _, err := bw.Write(data); err != nil {
			return err
		}
		pos += uint32(len(data))
	}
	_ = rowBytes

	comp := uint16(1)
	if compress {
		comp = 5
	}
	entries := []ifdEntry{
		{256, 4, 1, u32s(uint32(w))},
		{257, 4, 1, u32s(uint32(h))},
		{258, 3, 3, u16s(16, 16, 16)},
		{259, 3, 1, u16s(comp)},
		{262, 3, 1, u16s(2)},
	}
	if meta.Description != "" {
		entries = append(entries, ifdEntry{270, 2, uint32(len(meta.Description) + 1), ascii(meta.Description)})
	}
	if meta.Make != "" {
		entries = append(entries, ifdEntry{271, 2, uint32(len(meta.Make) + 1), ascii(meta.Make)})
	}
	if meta.Model != "" {
		entries = append(entries, ifdEntry{272, 2, uint32(len(meta.Model) + 1), ascii(meta.Model)})
	}
	entries = append(entries,
		ifdEntry{273, 4, uint32(nStrips), u32s(offsets...)},
		ifdEntry{277, 3, 1, u16s(3)},
		ifdEntry{278, 4, 1, u32s(rowsPerStrip)},
		ifdEntry{279, 4, uint32(nStrips), u32s(counts...)},
		ifdEntry{284, 3, 1, u16s(1)},
	)
	if meta.Software != "" {
		entries = append(entries, ifdEntry{305, 2, uint32(len(meta.Software) + 1), ascii(meta.Software)})
	}
	if compress {
		entries = append(entries, ifdEntry{317, 3, 1, u16s(2)})
	}
	if len(meta.ICC) > 0 {
		entries = append(entries, ifdEntry{34675, 7, uint32(len(meta.ICC)), meta.ICC})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].tag < entries[j].tag })
	if pos%2 == 1 {
		bw.WriteByte(0)
		pos++
	}
	ifdOff := pos
	ifdSize := uint32(2 + 12*len(entries) + 4)
	extra := ifdOff + ifdSize
	var ext []byte
	ifd := make([]byte, 0, ifdSize)
	ifd = append(ifd, u16s(uint16(len(entries)))...)
	for _, e := range entries {
		ifd = append(ifd, u16s(e.tag, e.typ)...)
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
	if _, err := bw.Write(ifd); err != nil {
		return err
	}
	if _, err := bw.Write(ext); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if _, err := f.WriteAt(u32s(ifdOff), 4); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

// u16bytes reinterprets samples as bytes (host is little-endian x86/arm64).
func u16bytes(s []uint16) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*2)
}

// WriteTIFF16Description embeds a versioned JSON ImageDescription (tag 270).
// Existing pixel encoding and compression are unchanged; callers may preserve
// arbitrary descriptions while rewriting hair edits.
func WriteTIFF16Description(path string, w, h int, pix []uint16, lzw bool, make_, model, software, description string) error {
	return writeTIFF(path, w, h, pix, lzw, tiffMeta{Make: make_, Model: model, Software: software, Description: description})
}
