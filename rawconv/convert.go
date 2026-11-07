// Package rawconv converts camera RAW files into linear 16-bit TIFFs using the
// same LibRaw settings as open_make_tiff (github.com/...open_make_tiff).
package rawconv

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed icc/*
var iccFS embed.FS

// ICCProfile is an optional profile embedded as TIFF tag 34675.
type ICCProfile struct {
	Key, Name, File string
}

// ICCProfiles lists the profiles shipped with open_make_tiff. Key "" = none
// (open_make_tiff default).
var ICCProfiles = []ICCProfile{
	{"", "None (default)", ""},
	{"sRGB", "sRGB", "sRGB.icc"},
	{"AdobeRGB1998", "Adobe RGB 1998", "AdobeRGB1998.icc"},
	{"DisplayP3", "Display P3", "DisplayP3.icc"},
	{"ProPhoto", "ProPhoto", "ProPhoto.icm"},
	{"BT2020", "BT.2020", "BT2020.icc"},
	{"HasselbladRGB", "Hasselblad RGB", "HasselbladRGB.icc"},
	{"FlextightInput", "Flextight Input", "FlextightInput.icc"},
}

func iccData(key string) ([]byte, error) {
	for _, p := range ICCProfiles {
		if p.Key == key {
			if p.File == "" {
				return nil, nil
			}
			return iccFS.ReadFile("icc/" + p.File)
		}
	}
	return nil, fmt.Errorf("unknown ICC profile %q", key)
}

// Options mirrors open_make_tiff's config.
type Options struct {
	Subfolder bool   // write into <raw dir>/make_tiff
	LZW       bool   // LZW + horizontal predictor
	ICC       string // key in ICCProfiles, "" = none
	UseDNG    bool   // go through Adobe DNG Converter when installed
	DNGExe    string // override path (optional)
	Software  string
}

// Result describes a finished conversion.
type Result struct {
	Dst           string
	Width, Height int
	Path          string // "LibRaw" or "Adobe DNG Converter + LibRaw"
	Note          string // e.g. DNG fallback reason
	CamMul        [4]float32
	Elapsed       time.Duration
}

// ErrDstExists is returned when the TIFF already exists (it is never overwritten).
var ErrDstExists = errors.New("TIFF already exists")

var rawExts = map[string]bool{
	".raf": true, ".nef": true, ".nrw": true, ".cr2": true, ".cr3": true, ".crw": true,
	".arw": true, ".srf": true, ".sr2": true, ".dng": true, ".orf": true, ".rw2": true,
	".pef": true, ".srw": true, ".3fr": true, ".fff": true, ".iiq": true, ".mos": true,
	".erf": true, ".mef": true, ".mrw": true, ".x3f": true, ".kdc": true, ".dcr": true,
	".rwl": true, ".gpr": true, ".raw": true,
}

// IsRaw reports whether path has a supported RAW extension.
func IsRaw(path string) bool { return rawExts[strings.ToLower(filepath.Ext(path))] }

// DstPath returns where Convert writes the TIFF for src.
func DstPath(src string, subfolder bool) string {
	dir := filepath.Dir(src)
	if subfolder {
		dir = filepath.Join(dir, "make_tiff")
	}
	return filepath.Join(dir, filepath.Base(src)+".tiff")
}

// DefaultWorkers matches open_make_tiff: half the logical CPUs, at least 1.
func DefaultWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
}

func token() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Convert converts one RAW file. ctx only cancels the Adobe DNG Converter step.
func Convert(ctx context.Context, src string, o Options) (*Result, error) {
	start := time.Now()
	src, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	dst := DstPath(src, o.Subfolder)
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrDstExists, dst)
	}
	icc, err := iccData(o.ICC)
	if err != nil {
		return nil, err
	}
	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, err
	}
	name := filepath.Base(src)
	tok := token()
	res := &Result{Dst: dst, Path: "LibRaw"}

	var img *Image
	if o.UseDNG {
		exe := o.DNGExe
		if exe == "" {
			exe = DNGConverterPath()
		}
		if exe == "" {
			res.Note = "Adobe DNG Converter not installed, used LibRaw directly"
		} else {
			img, err = decodeViaDNG(ctx, exe, src, dstDir, name, tok)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				res.Note = "DNG Converter failed (" + err.Error() + "), used LibRaw directly"
				img = nil
			} else {
				res.Path = "Adobe DNG Converter + LibRaw"
			}
		}
	}
	if img == nil {
		img, err = decode(src, false)
		if err != nil {
			return nil, err
		}
	}
	defer img.Free()

	tmp := filepath.Join(dstDir, fmt.Sprintf("%s_%s.int.tiff", name, tok))
	meta := tiffMeta{Make: img.Make, Model: img.Model, Software: o.Software, ICC: icc}
	if err := writeTIFF(tmp, img.Width, img.Height, img.Pix, o.LZW, meta); err != nil {
		return nil, fmt.Errorf("write TIFF: %w", err)
	}
	if _, err := os.Stat(dst); err == nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("%w: %s", ErrDstExists, dst)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	res.Width, res.Height = img.Width, img.Height
	res.CamMul = img.CamMul
	res.Elapsed = time.Since(start)
	return res, nil
}
