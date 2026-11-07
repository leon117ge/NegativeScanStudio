package hdrscan

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"basetool/rawconv"
)

// TIFFInfo is what the hair tool keeps when it rewrites an HDR TIFF.
type TIFFInfo struct {
	Make, Model, Software string
	Description           string
	LZW                   bool
}

// ReadTIFFInfo reads ImageDescription, Make/Model/Software and compression of IFD0.
func ReadTIFFInfo(path string) (TIFFInfo, error) {
	var ti TIFFInfo
	f, err := os.Open(path)
	if err != nil {
		return ti, err
	}
	defer f.Close()
	var hdr [8]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return ti, err
	}
	var bo binary.ByteOrder
	switch string(hdr[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return ti, fmt.Errorf("%s: not a TIFF", filepath.Base(path))
	}
	off := int64(bo.Uint32(hdr[4:]))
	var nb [2]byte
	if _, err := f.ReadAt(nb[:], off); err != nil {
		return ti, err
	}
	n := int(bo.Uint16(nb[:]))
	ents := make([]byte, 12*n)
	if _, err := f.ReadAt(ents, off+2); err != nil {
		return ti, err
	}
	str := func(e []byte) string {
		cnt := int(bo.Uint32(e[4:]))
		if cnt <= 0 || cnt > 1<<20 {
			return ""
		}
		b := make([]byte, cnt)
		if cnt <= 4 {
			copy(b, e[8:8+cnt])
		} else if _, err := f.ReadAt(b, int64(bo.Uint32(e[8:]))); err != nil {
			return ""
		}
		return strings.TrimRight(string(b), "\x00")
	}
	for i := 0; i < n; i++ {
		e := ents[12*i : 12*i+12]
		switch bo.Uint16(e) {
		case 259:
			ti.LZW = bo.Uint16(e[8:]) == 5
		case 270:
			ti.Description = str(e)
		case 271:
			ti.Make = str(e)
		case 272:
			ti.Model = str(e)
		case 305:
			ti.Software = str(e)
		}
	}
	return ti, nil
}

var hdrNameRE = regexp.MustCompile(`(?i)_HDR(_\d+)?\.tiff?$`)

// HairSavePath is where the hair tool saves an edit of src: the same file
// when src is an HDR output of this program (<name>_HDR.tiff or
// <name>_HDR_<n>.tiff), otherwise a new <name>_HDR.tiff next to it
// (numbered, never overwriting) so a source file is never replaced.
func HairSavePath(src string) (path string, inPlace bool) {
	if hdrNameRE.MatchString(filepath.Base(src)) {
		return src, true
	}
	return uniqueFile(HDRPath(filepath.Dir(src), src)), false
}

// HairSoftware appends the hair-removal note once to a Software tag.
func HairSoftware(sw, app string, edits int) string {
	note := fmt.Sprintf("hair removal (%d selection", edits)
	if edits != 1 {
		note += "s"
	}
	note += ", " + app + ")"
	if i := strings.Index(sw, ", hair removal ("); i >= 0 {
		sw = sw[:i]
	}
	if sw == "" {
		return note
	}
	return sw + ", " + note
}

// SaveHairTIFF writes pix to path through a temporary file in the same
// folder that then replaces path, so a failed write never leaves a broken
// HDR behind.
func SaveHairTIFF(path string, w, h int, pix []uint16, ti TIFFInfo) error {
	tmp := path + ".hairtmp"
	os.Remove(tmp)
	if err := rawconv.WriteTIFF16Description(tmp, w, h, pix, ti.LZW, ti.Make, ti.Model, ti.Software, ti.Description); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
