package cropper

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/tiff"
)

// Image is a decoded RGB picture. Pix holds 3 samples per pixel on a 16-bit
// scale (8-bit sources are stored as v*257 so they round-trip exactly).
type Image struct {
	W, H   int
	Bits   int    // 8 or 16 (source bit depth, kept on export)
	Format string // "tiff", "jpeg", "png"
	Pix    []uint16
	meta   metadata
}

type metadata struct {
	tiffTags  []rawTag // copied IFD0 tags, little-endian payload
	jpegSegs  [][]byte // APPn segments (marker included) to re-inject
	pngChunks [][]byte // ancillary chunks (len+type+data+crc) to re-inject
	hasICC    bool
}

// HasICC reports whether an embedded colour profile was found (and will be kept).
func (im *Image) HasICC() bool { return im.meta.hasICC }

// FormatOf returns the format implied by the file extension.
func FormatOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tif", ".tiff":
		return "tiff"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".png":
		return "png"
	}
	return ""
}

// Supported reports whether path has an extension the cropper can read.
func Supported(path string) bool { return FormatOf(path) != "" }

// Load decodes a TIFF (8/16-bit), JPEG or PNG file.
func Load(path string) (*Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var src image.Image
	format := FormatOf(path)
	switch {
	case len(data) > 4 && (string(data[:4]) == "II*\x00" || string(data[:4]) == "MM\x00*"):
		format = "tiff"
		src, err = tiff.Decode(bytes.NewReader(data))
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8:
		format = "jpeg"
		src, err = jpeg.Decode(bytes.NewReader(data))
	case len(data) > 8 && string(data[1:4]) == "PNG":
		format = "png"
		src, err = png.Decode(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("unsupported file type")
	}
	if err != nil {
		return nil, err
	}
	im := toRGB16(src)
	im.Format = format
	switch format {
	case "tiff":
		im.meta.tiffTags, im.meta.hasICC = readTIFFTags(data)
		if b := tiffBitsPerSample(data); b == 8 {
			im.Bits = 8
		}
	case "jpeg":
		im.meta.jpegSegs, im.meta.hasICC = readJPEGSegments(data)
		im.Bits = 8
	case "png":
		im.meta.pngChunks, im.meta.hasICC = readPNGChunks(data)
	}
	return im, nil
}

func toRGB16(src image.Image) *Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	im := &Image{W: w, H: h, Bits: 16, Pix: make([]uint16, w*h*3)}
	switch s := src.(type) {
	case *image.RGBA64:
		for y := 0; y < h; y++ {
			row := s.Pix[y*s.Stride:]
			o := im.Pix[y*w*3:]
			for x := 0; x < w; x++ {
				o[x*3] = uint16(row[x*8])<<8 | uint16(row[x*8+1])
				o[x*3+1] = uint16(row[x*8+2])<<8 | uint16(row[x*8+3])
				o[x*3+2] = uint16(row[x*8+4])<<8 | uint16(row[x*8+5])
			}
		}
		return im
	case *image.NRGBA64:
		for y := 0; y < h; y++ {
			row := s.Pix[y*s.Stride:]
			o := im.Pix[y*w*3:]
			for x := 0; x < w; x++ {
				o[x*3] = uint16(row[x*8])<<8 | uint16(row[x*8+1])
				o[x*3+1] = uint16(row[x*8+2])<<8 | uint16(row[x*8+3])
				o[x*3+2] = uint16(row[x*8+4])<<8 | uint16(row[x*8+5])
			}
		}
		return im
	case *image.Gray16:
		for y := 0; y < h; y++ {
			row := s.Pix[y*s.Stride:]
			o := im.Pix[y*w*3:]
			for x := 0; x < w; x++ {
				v := uint16(row[x*2])<<8 | uint16(row[x*2+1])
				o[x*3], o[x*3+1], o[x*3+2] = v, v, v
			}
		}
		return im
	case *image.Gray:
		im.Bits = 8
		for y := 0; y < h; y++ {
			row := s.Pix[y*s.Stride:]
			o := im.Pix[y*w*3:]
			for x := 0; x < w; x++ {
				v := uint16(row[x]) * 257
				o[x*3], o[x*3+1], o[x*3+2] = v, v, v
			}
		}
		return im
	}
	// 8-bit family (RGBA, NRGBA, YCbCr, CMYK, Paletted...): convert via NRGBA.
	im.Bits = 8
	n, ok := src.(*image.NRGBA)
	if !ok {
		if r, ok2 := src.(*image.RGBA); ok2 {
			n = &image.NRGBA{Pix: r.Pix, Stride: r.Stride, Rect: r.Rect} // opaque images: identical
		} else {
			n = image.NewNRGBA(image.Rect(0, 0, w, h))
			draw.Draw(n, n.Rect, src, b.Min, draw.Src)
		}
	}
	for y := 0; y < h; y++ {
		row := n.Pix[y*n.Stride:]
		o := im.Pix[y*w*3:]
		for x := 0; x < w; x++ {
			o[x*3] = uint16(row[x*4]) * 257
			o[x*3+1] = uint16(row[x*4+1]) * 257
			o[x*3+2] = uint16(row[x*4+2]) * 257
		}
	}
	return im
}

// ---------------------------------------------------------------- TIFF tags

type rawTag struct {
	id, typ uint16
	count   uint32
	data    []byte // little-endian payload
}

// tags copied from the source IFD0 to the output
var keepTIFFTags = map[uint16]bool{
	270: true, 271: true, 272: true, 274: true, 282: true, 283: true, 296: true,
	305: true, 306: true, 315: true, 316: true, 700: true, 33432: true, 34675: true,
}

var tiffTypeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8, 11: 4, 12: 8}

func tiffIFD0(data []byte) (binary.ByteOrder, uint32, bool) {
	if len(data) < 8 {
		return nil, 0, false
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if data[0] == 'M' {
		bo = binary.BigEndian
	}
	off := bo.Uint32(data[4:])
	if int(off)+2 > len(data) {
		return nil, 0, false
	}
	return bo, off, true
}

func tiffBitsPerSample(data []byte) int {
	bo, off, ok := tiffIFD0(data)
	if !ok {
		return 0
	}
	n := int(bo.Uint16(data[off:]))
	for i := 0; i < n; i++ {
		e := int(off) + 2 + i*12
		if e+12 > len(data) {
			break
		}
		if bo.Uint16(data[e:]) == 258 {
			cnt := bo.Uint32(data[e+4:])
			if cnt <= 2 {
				return int(bo.Uint16(data[e+8:]))
			}
			p := bo.Uint32(data[e+8:])
			if int(p)+2 <= len(data) {
				return int(bo.Uint16(data[p:]))
			}
		}
	}
	return 0
}

func readTIFFTags(data []byte) ([]rawTag, bool) {
	bo, off, ok := tiffIFD0(data)
	if !ok {
		return nil, false
	}
	n := int(bo.Uint16(data[off:]))
	var out []rawTag
	icc := false
	for i := 0; i < n; i++ {
		e := int(off) + 2 + i*12
		if e+12 > len(data) {
			break
		}
		id := bo.Uint16(data[e:])
		if !keepTIFFTags[id] {
			continue
		}
		typ := bo.Uint16(data[e+2:])
		cnt := bo.Uint32(data[e+4:])
		sz := tiffTypeSize[typ]
		if sz == 0 || cnt == 0 || cnt > 1<<26 {
			continue
		}
		total := sz * int(cnt)
		var p []byte
		if total <= 4 {
			p = data[e+8 : e+8+total]
		} else {
			vo := int(bo.Uint32(data[e+8:]))
			if vo < 0 || vo+total > len(data) {
				continue
			}
			p = data[vo : vo+total]
		}
		le := make([]byte, total)
		copy(le, p)
		if bo == binary.BigEndian { // swap multi-byte elements to little-endian
			unit := sz
			if typ == 5 || typ == 10 {
				unit = 4
			}
			if unit > 1 {
				for k := 0; k+unit <= total; k += unit {
					for a, b := k, k+unit-1; a < b; a, b = a+1, b-1 {
						le[a], le[b] = le[b], le[a]
					}
				}
			}
		}
		if id == 34675 {
			icc = true
		}
		out = append(out, rawTag{id, typ, cnt, le})
	}
	return out, icc
}

// --------------------------------------------------------------- JPEG / PNG

func readJPEGSegments(data []byte) ([][]byte, bool) {
	var segs [][]byte
	icc := false
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			break
		}
		m := data[i+1]
		if m == 0xDA || m == 0xD9 {
			break
		}
		l := int(data[i+2])<<8 | int(data[i+3])
		if i+2+l > len(data) {
			break
		}
		// APP1 (Exif / XMP), APP2 (ICC), APP13 (IPTC), APP14 (Adobe) is left to the encoder
		if m == 0xE1 || m == 0xE2 || m == 0xED {
			seg := append([]byte(nil), data[i:i+2+l]...)
			if m == 0xE2 && bytes.HasPrefix(seg[4:], []byte("ICC_PROFILE")) {
				icc = true
			}
			segs = append(segs, seg)
		}
		i += 2 + l
	}
	return segs, icc
}

func readPNGChunks(data []byte) ([][]byte, bool) {
	var out [][]byte
	icc := false
	i := 8
	for i+12 <= len(data) {
		l := int(binary.BigEndian.Uint32(data[i:]))
		if l < 0 || i+12+l > len(data) {
			break
		}
		typ := string(data[i+4 : i+8])
		switch typ {
		case "iCCP", "sRGB", "gAMA", "cHRM", "pHYs", "tEXt", "iTXt", "zTXt", "eXIf":
			out = append(out, append([]byte(nil), data[i:i+12+l]...))
			if typ == "iCCP" {
				icc = true
			}
		case "IEND":
			return out, icc
		}
		i += 12 + l
	}
	return out, icc
}

// ------------------------------------------------------------------ saving

// SaveOptions controls the encoder.
type SaveOptions struct {
	LZW         bool // TIFF: LZW + horizontal predictor
	JPEGQuality int  // 1..100
	Software    string
}

// Save writes im next to the metadata it was loaded with, in the same format
// and bit depth.
func Save(path string, im *Image, opt SaveOptions) error {
	var buf bytes.Buffer
	var err error
	switch im.Format {
	case "tiff":
		err = encodeTIFF(&buf, im, opt)
	case "jpeg":
		err = encodeJPEG(&buf, im, opt)
	case "png":
		err = encodePNG(&buf, im)
	default:
		err = errors.New("unknown output format")
	}
	if err != nil {
		return err
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	if _, err = bw.Write(buf.Bytes()); err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func to8(v uint16) uint8 { return uint8((uint32(v)*255 + 32767) / 65535) }

func encodeJPEG(w *bytes.Buffer, im *Image, opt SaveOptions) error {
	rgba := image.NewRGBA(image.Rect(0, 0, im.W, im.H))
	for i, j := 0, 0; i < len(im.Pix); i, j = i+3, j+4 {
		rgba.Pix[j], rgba.Pix[j+1], rgba.Pix[j+2], rgba.Pix[j+3] = to8(im.Pix[i]), to8(im.Pix[i+1]), to8(im.Pix[i+2]), 255
	}
	q := opt.JPEGQuality
	if q <= 0 || q > 100 {
		q = 95
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, rgba, &jpeg.Options{Quality: q}); err != nil {
		return err
	}
	b := enc.Bytes()
	w.Write(b[:2]) // SOI
	for _, s := range im.meta.jpegSegs {
		w.Write(s)
	}
	w.Write(b[2:])
	return nil
}

func encodePNG(w *bytes.Buffer, im *Image) error {
	var img image.Image
	if im.Bits == 8 {
		n := image.NewNRGBA(image.Rect(0, 0, im.W, im.H))
		for i, j := 0, 0; i < len(im.Pix); i, j = i+3, j+4 {
			n.Pix[j], n.Pix[j+1], n.Pix[j+2], n.Pix[j+3] = to8(im.Pix[i]), to8(im.Pix[i+1]), to8(im.Pix[i+2]), 255
		}
		img = n
	} else {
		n := image.NewNRGBA64(image.Rect(0, 0, im.W, im.H))
		for i, j := 0, 0; i < len(im.Pix); i, j = i+3, j+8 {
			for c := 0; c < 3; c++ {
				n.Pix[j+2*c], n.Pix[j+2*c+1] = byte(im.Pix[i+c]>>8), byte(im.Pix[i+c])
			}
			n.Pix[j+6], n.Pix[j+7] = 255, 255
		}
		img = n
	}
	var enc bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&enc, img); err != nil {
		return err
	}
	b := enc.Bytes()
	// signature (8) + IHDR chunk (12+13)
	ih := 8 + 12 + 13
	w.Write(b[:ih])
	for _, c := range im.meta.pngChunks {
		w.Write(c)
	}
	w.Write(b[ih:])
	return nil
}
