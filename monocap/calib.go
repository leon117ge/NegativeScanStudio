// Package monocap runs the Hikrobot mono-trichrome capture sequence
// (light switching, settle check, exposure brackets, frame averaging,
// dark / flat calibration) independently of the GUI, so the simulator can
// drive it end to end in tests.
package monocap

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const calMagic = "NSSCAL1\n"

// CalHeader describes one stored calibration frame.
type CalHeader struct {
	Kind         string  `json:"kind"` // "dark" or "flat"
	ID           string  `json:"id"`
	Created      string  `json:"created"`
	Software     string  `json:"software,omitempty"`
	Camera       string  `json:"camera,omitempty"`
	W            int     `json:"w"`
	H            int     `json:"h"`
	PixelFormat  string  `json:"pixel_format"`
	ExposureUS   float64 `json:"exposure_us"`
	GainDB       float64 `json:"gain_db"`
	BlackLevelDN float64 `json:"black_level_dn"`
	Black16      float64 `json:"black_16bit"`
	Frames       int     `json:"frames"`
	Channel      int     `json:"channel"` // flat: 0 R, 1 G, 2 B; dark: -1
	LightRGB     [3]int  `json:"light_rgb,omitempty"`
	Mean16       float64 `json:"mean_16bit"`                 // dark: mean; flat: centre median before normalising
	ReadNoise16  float64 `json:"read_noise_16bit,omitempty"` // dark: temporal noise of one grab
	DarkUsed     string  `json:"dark_used,omitempty"`        // flat: dark or pedestal subtracted
}

// CalFrame is a header plus its 16-bit pixels (dark: raw DN incl. the
// pedestal; flat: gain map, hdrscan.FlatScale = 1.0).
type CalFrame struct {
	CalHeader
	Pix []uint16
}

// Store is a folder of calibration frames.
type Store struct{ Dir string }

var tagTime = func() time.Time { return time.Now() }

func shanghai(t time.Time) string {
	return t.In(time.FixedZone("UTC+8", 8*3600)).Format(time.RFC3339)
}

func fmtTag(f string) string { return strings.ToLower(strings.TrimSpace(f)) }

func darkName(us, gain float64, pf string, w, h int) string {
	return fmt.Sprintf("dark_%.0fus_g%.1f_%s_%dx%d.nsscal", us, gain, fmtTag(pf), w, h)
}

func flatName(ch int, pf string, w, h int) string {
	return fmt.Sprintf("flat_%c_%s_%dx%d.nsscal", "RGB"[ch], fmtTag(pf), w, h)
}

// Save writes a calibration frame atomically.
func (s Store) Save(c CalFrame) (string, error) {
	if s.Dir == "" {
		return "", errors.New("no calibration folder")
	}
	if len(c.Pix) < c.W*c.H || c.W <= 0 || c.H <= 0 {
		return "", errors.New("calibration frame size mismatch")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	var name string
	switch c.Kind {
	case "dark":
		name = darkName(c.ExposureUS, c.GainDB, c.PixelFormat, c.W, c.H)
	case "flat":
		name = flatName(c.Channel, c.PixelFormat, c.W, c.H)
	default:
		return "", fmt.Errorf("unknown calibration kind %q", c.Kind)
	}
	hdr, err := json.Marshal(c.CalHeader)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.Dir, name)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	_, _ = w.WriteString(calMagic)
	_ = binary.Write(w, binary.LittleEndian, uint32(len(hdr)))
	_, _ = w.Write(hdr)
	buf := make([]byte, 2*c.W)
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			binary.LittleEndian.PutUint16(buf[2*x:], c.Pix[y*c.W+x])
		}
		if _, err := w.Write(buf); err != nil {
			f.Close()
			os.Remove(tmp)
			return "", err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func readHeader(path string) (CalHeader, *os.File, error) {
	var h CalHeader
	f, err := os.Open(path)
	if err != nil {
		return h, nil, err
	}
	magic := make([]byte, len(calMagic))
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != calMagic {
		f.Close()
		return h, nil, fmt.Errorf("%s: not a calibration file", filepath.Base(path))
	}
	var n uint32
	if err := binary.Read(f, binary.LittleEndian, &n); err != nil || n > 1<<20 {
		f.Close()
		return h, nil, fmt.Errorf("%s: bad header", filepath.Base(path))
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(f, b); err != nil {
		f.Close()
		return h, nil, err
	}
	if err := json.Unmarshal(b, &h); err != nil {
		f.Close()
		return h, nil, err
	}
	return h, f, nil
}

// Load reads a calibration frame.
func Load(path string) (*CalFrame, error) {
	h, f, err := readHeader(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if h.W <= 0 || h.H <= 0 || h.W*h.H > 100_000_000 {
		return nil, fmt.Errorf("%s: bad size", filepath.Base(path))
	}
	raw := make([]byte, 2*h.W*h.H)
	if _, err := io.ReadFull(bufio.NewReaderSize(f, 1<<20), raw); err != nil {
		return nil, fmt.Errorf("%s: truncated", filepath.Base(path))
	}
	pix := make([]uint16, h.W*h.H)
	for i := range pix {
		pix[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	return &CalFrame{CalHeader: h, Pix: pix}, nil
}

// List returns the headers of all calibration frames in the store.
func (s Store) List() []CalHeader {
	var out []CalHeader
	ents, _ := os.ReadDir(s.Dir)
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".nsscal") {
			continue
		}
		h, f, err := readHeader(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		f.Close()
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].ExposureUS < out[j].ExposureUS
	})
	return out
}

// FindDark loads the dark frame for an exposure (within 2 %), gain,
// pixel format and size, or returns nil.
func (s Store) FindDark(us, gain float64, pf string, w, h int) *CalFrame {
	if s.Dir == "" {
		return nil
	}
	for _, c := range s.List() {
		if c.Kind != "dark" || c.W != w || c.H != h || fmtTag(c.PixelFormat) != fmtTag(pf) || math.Abs(c.GainDB-gain) > 0.05 {
			continue
		}
		if math.Abs(c.ExposureUS/us-1) > 0.02 {
			continue
		}
		f, err := Load(filepath.Join(s.Dir, darkName(c.ExposureUS, c.GainDB, c.PixelFormat, c.W, c.H)))
		if err == nil {
			return f
		}
	}
	return nil
}

// FindFlat loads the flat field of a channel for a pixel format and size.
func (s Store) FindFlat(ch int, pf string, w, h int) *CalFrame {
	if s.Dir == "" {
		return nil
	}
	f, err := Load(filepath.Join(s.Dir, flatName(ch, pf, w, h)))
	if err != nil {
		return nil
	}
	return f
}

// Summary is a one-line English description of the store.
func (s Store) Summary() string {
	var darks []string
	flats := ""
	for _, c := range s.List() {
		switch c.Kind {
		case "dark":
			darks = append(darks, fmt.Sprintf("%.0f us", c.ExposureUS))
		case "flat":
			flats += string("RGB"[c.Channel])
		}
	}
	d := "no dark frames"
	if len(darks) > 0 {
		d = "darks at " + strings.Join(darks, ", ")
	}
	f := "no flat field"
	if flats != "" {
		f = "flats " + flats
	}
	return d + "; " + f
}

func sortFloat(v []float64) { sort.Float64s(v) }
