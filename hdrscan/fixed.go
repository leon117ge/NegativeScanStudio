package hdrscan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"basetool/basestat"
	"basetool/camera"
	"basetool/rawconv"
)

// Fixed frame 0 (v0.63): frame 0 is shot at a chosen ISO and 1/60 s with no
// metering search; the brackets keep the ISO and scale the shutter only.
// "Record base" shoots exactly this frame 0 at the film-base DB reference.

// FixedShutter is the default frame-0 shutter in Fixed mode (1/60 s).
const FixedShutter = camera.Shutter1_60

// FixedShutters (v0.81) are the Fixed shutters the user can choose:
// 1/60 s (default) and 1/125 s (the X-T3 value for "1/120 s").
var FixedShutters = []uint32{camera.Shutter1_60, camera.Shutter1_125}

// ValidFixedShutter reports whether sh is one of FixedShutters.
func ValidFixedShutter(sh uint32) bool {
	for _, v := range FixedShutters {
		if v == sh {
			return true
		}
	}
	return false
}

// fixedShutter is the session's Fixed shutter (FixedShutter if unset).
func (s *Session) fixedShutter() uint32 {
	if ValidFixedShutter(s.Fixed) {
		return s.Fixed
	}
	return FixedShutter
}

// FixedExposure is the Fixed frame-0 exposure at iso.
func (s *Session) FixedExposure(iso int) camera.Exposure {
	return camera.Exposure{ISO: iso, Shutter: s.fixedShutter()}
}

// DB reference conditions of "Record base".
const (
	RefISO      = 250
	RefAperture = 4.0
)

// Base-level warning limits for a fixed frame 0 (fraction of full scale).
const (
	FixedClip = 0.95
	FixedLow  = 0.15
)

// FixedWarnings returns the clip / low-level warnings of a fixed frame 0.
func FixedWarnings(r Reading) []string {
	var w []string
	if r.Clipped || r.Max > FixedClip || r.Peak >= PeakMax {
		w = append(w, fmt.Sprintf("base CLIPS (%.3f FS, peak %.3f) - use a lower ISO or less light", r.Max, r.Peak))
	} else if r.Max < FixedLow {
		w = append(w, fmt.Sprintf("base is very low (%.3f FS) - use a higher ISO or more light", r.Max))
	}
	if r.Warn != "" {
		w = append(w, r.Warn)
	}
	return w
}

// PureBase reports whether a reading is a usable base value for the DB: an
// auto-found base (or the user's rectangle) without fallback and not clipped.
func PureBase(r Reading) bool {
	return r.Warn == "" && !r.Clipped && r.Max > 0 && r.Max <= FixedClip && r.Peak < PeakMax
}

// LockFixed sets ISO iso + the Fixed shutter (1/60 s or 1/125 s), shoots one probe (the frame-0 base
// reading, same code as metering) and locks that exposure. Warnings do not
// prevent the lock.
func (s *Session) LockFixed(iso int) (camera.Exposure, Reading, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Locked = nil
	e, r, err := s.fixedProbe(iso, "frame 0 (fixed)")
	if err != nil {
		return e, r, nil, err
	}
	s.Reading = r
	w := FixedWarnings(r)
	s.Locked = &e
	s.say("Locked frame 0 at %s (Fixed: no metering), base R %.4f G %.4f B %.4f, max %.3f FS", e, r.RGB[0], r.RGB[1], r.RGB[2], r.Max)
	for _, m := range w {
		s.say("  WARNING: %s", m)
	}
	return e, r, w, nil
}

func (s *Session) fixedProbe(iso int, why string) (camera.Exposure, Reading, error) {
	want := s.FixedExposure(iso)
	if err := s.checkManual(); err != nil {
		s.say("NOT SET: %v", err)
		return want, Reading{}, err
	}
	if err := s.setExposure(want, why); err != nil {
		return want, Reading{}, err
	}
	r, e, err := s.probe()
	if err != nil {
		return e, r, err
	}
	s.say("Probe at %s: %s", e, r)
	if e != want {
		return e, r, fmt.Errorf("camera is at %s, not %s - set M mode and a fixed ISO (not Auto)", e, want)
	}
	return e, r, nil
}

// BaseRecord is a standalone frame 0 shot for the film-base DB.
type BaseRecord struct {
	Raw      string
	Exposure camera.Exposure
	Reading  Reading
	Warnings []string
	EXIF     string
	// Base1 is the base on module 1's scale (linear TIFF / 65535, leak
	// rejection, per-channel median over the metered region) - the value
	// stored in the film-base DB and compared with frame 0.
	Base1  [3]float64
	Base1N int
}

// Module1Base measures a RAW's base region with module 1's statistic.
func Module1Base(path string, reg basestat.Region, leakPct float64) ([3]float64, int, error) {
	if reg.Rect.Empty() {
		return [3]float64{}, 0, errors.New("no base region")
	}
	pl, err := Load(path)
	if err != nil {
		return [3]float64{}, 0, err
	}
	defer pl.Free()
	if leakPct <= 0 {
		leakPct = 20
	}
	rgb, _, n, err := basestat.Measure(basestat.PlaneGetter(pl.Pix, pl.W), pl.W, pl.H, reg, leakPct)
	return rgb, n, err
}

// RecordBase shoots exactly a fixed frame 0 at ISO iso (RefISO for the DB) +
// 1/60 s, measures the base with the frame-0 code and keeps the RAW as
// <roll>_<date>_<time>_<NNNN>_BASE.<ext> in dir (never overwriting). The
// lock is cleared unless the camera is left at the locked exposure.
func (s *Session) RecordBase(dir, roll string, iso int, last int, leakPct float64) (*BaseRecord, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := time.Now()
	e, r, err := s.fixedProbe(iso, "record base")
	if err != nil {
		return nil, last, err
	}
	if s.Locked != nil && *s.Locked != e {
		s.Locked = nil
		s.say("Note: the camera is now at %s - frame 0 must be metered/locked again before the next scan", e)
	}
	rec := &BaseRecord{Exposure: e, Reading: r, Warnings: FixedWarnings(r)}
	var merr error
	rec.Base1, rec.Base1N, merr = Module1Base(s.LastProbePath, r.Region, leakPct)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return rec, last, err
	}
	base, n := NextShot(dir, roll, t, last)
	src := s.LastProbePath
	dst, err := renameNoClobber(src, filepath.Join(dir, base+"_BASE"+filepath.Ext(src)))
	if err != nil {
		// cross-device: copy
		if b, rerr := os.ReadFile(src); rerr == nil {
			dst = uniqueFile(filepath.Join(dir, base+"_BASE"+filepath.Ext(src)))
			if werr := os.WriteFile(dst, b, 0o644); werr != nil {
				return rec, last, werr
			}
			os.Remove(src)
		} else {
			return rec, last, err
		}
	}
	s.LastProbePath = "" // kept: not a probe any more
	rec.Raw = dst
	rec.EXIF = EXIFCheck(dst, e)
	if merr != nil {
		return rec, n, fmt.Errorf("base record saved as %s but the base could not be measured: %w", filepath.Base(dst), merr)
	}
	s.say("Base record saved as %s - base R %.4f G %.4f B %.4f (module 1 scale, %d samples; frame-0 reading %.3f FS) - %s", filepath.Base(dst), rec.Base1[0], rec.Base1[1], rec.Base1[2], rec.Base1N, r.Max, rec.EXIF)
	for _, m := range rec.Warnings {
		s.say("  WARNING: %s", m)
	}
	return rec, n, nil
}

// EXIFCheck reads ISO and shutter from a RAW and compares them with e.
func EXIFCheck(path string, e camera.Exposure) string {
	iso, sh, err := rawconv.ReadExposure(path)
	if err != nil {
		return "RAF EXIF: unreadable (" + err.Error() + ")"
	}
	msg := fmt.Sprintf("RAF EXIF: ISO %.0f, shutter %s", iso, shutterText(sh))
	want := float64(e.Shutter) / 1e6
	if int(iso+0.5) != e.ISO || sh <= 0 || sh/want > 1.1 || want/sh > 1.1 {
		msg += fmt.Sprintf(" - DIFFERS from the set %s", e)
	} else {
		msg += " (matches)"
	}
	return msg
}

func shutterText(sec float64) string {
	if sec <= 0 {
		return "?"
	}
	if sec < 1 {
		return fmt.Sprintf("1/%.0f s", 1/sec)
	}
	return fmt.Sprintf("%.1f s", sec)
}

// MeasureFrame0 measures a frame-0 RAW with the metering code (the same
// reading as the probe), for "Save last frame 0 to DB".
func MeasureFrame0(path string, roi Rect) (Reading, error) {
	f, err := rawconv.ReadRawFrame(path)
	if err != nil {
		return Reading{}, err
	}
	return MeasureBase(f, roi)
}

// DespeckleFile cleans an existing 16-bit RGB TIFF (or RAW) with the same
// dust removal as the HDR merge and writes <stem>_dust.tiff next to it (the
// input is never changed; _2, _3 ... if that name exists). The film base is
// found automatically and protected.
func DespeckleFile(path string, level int, software string) (string, DustStats, error) {
	if level != DustLight && level != DustStrong {
		return "", DustStats{}, fmt.Errorf("dust level must be light or strong")
	}
	p, err := Load(path)
	if err != nil {
		return "", DustStats{}, err
	}
	defer p.Free()
	pix, ds := despeckleHDR(p, p.Pix, ProcessOptions{Dust: level})
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	out := uniqueFile(stem + "_dust.tiff")
	ti, _ := ReadTIFFInfo(path)
	if err := rawconv.WriteTIFF16Description(out, p.W, p.H, pix, true, p.Make, p.Model, software+", dust removal "+DustName(level), ti.Description); err != nil {
		return "", ds, err
	}
	return out, ds, nil
}

// DespeckleFileDebug cleans the HDR TIFF at path like DespeckleFile but
// writes nothing: it returns the cleaned pixels, the size and every
// candidate spot (for tests and tuning).
func DespeckleFileDebug(path string, level int) ([]uint16, int, int, DustStats, []DustSpot, error) {
	p, err := Load(path)
	if err != nil {
		return nil, 0, 0, DustStats{}, nil, err
	}
	defer p.Free()
	pix, ds, spots := despeckleHDRDebug(p, p.Pix, ProcessOptions{Dust: level}, true)
	return pix, p.W, p.H, ds, spots, nil
}

// ParseDust maps "off", "light", "strong" to DustOff/DustLight/DustStrong.
func ParseDust(s string) (int, bool) {
	for l := DustOff; l <= DustStrong; l++ {
		if strings.EqualFold(strings.TrimSpace(s), DustName(l)) {
			return l, true
		}
	}
	return DustOff, false
}
