package develop

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// The print-film LUTs that ship inside the program. Drop the real files
// over these names (see luts/README.txt) and rebuild: nothing else changes.
//
//go:embed luts/*.cube
var embeddedLUTs embed.FS

// EmbeddedName returns the embedded file name for a projection white (D55/D60/D65).
func EmbeddedName(white string) string { return "luts/Kodak_2383_" + white + ".cube" }

// ResolveName is the Film Looks file name Resolve installs.
func ResolveName(white string) string { return "Rec709 Kodak 2383 " + white + ".cube" }

// ResolveDirs are the folders searched by "auto-detect".
func ResolveDirs() []string {
	var d []string
	if p := os.Getenv("ProgramData"); p != "" {
		d = append(d, filepath.Join(p, "Blackmagic Design", "DaVinci Resolve", "Support", "LUT", "Film Looks"))
	}
	d = append(d, `C:\ProgramData\Blackmagic Design\DaVinci Resolve\Support\LUT\Film Looks`)
	if p := os.Getenv("APPDATA"); p != "" {
		d = append(d, filepath.Join(p, "Blackmagic Design", "DaVinci Resolve", "Support", "LUT", "Film Looks"))
	}
	d = append(d, "/Library/Application Support/Blackmagic Design/DaVinci Resolve/LUT/Film Looks")
	return d
}

// FindResolveLUT returns the path of Resolve's 2383 LUT for white ("" = not found).
func FindResolveLUT(white string) string {
	for _, d := range ResolveDirs() {
		p := filepath.Join(d, ResolveName(white))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// LUT is a .cube LUT (optional 1D shaper, optional 3D).
type LUT struct {
	Title       string
	Source      string // where it was loaded from
	Placeholder bool   // "# FBM_PLACEHOLDER" header: not the real print LUT
	N1          int
	L1          []float32 // N1*3
	N3          int
	L3          []float32 // N3^3*3, red fastest
	Min, Max    [3]float32
}

// ParseCube reads an Adobe/Resolve .cube file.
func ParseCube(r io.Reader, source string) (*LUT, error) {
	l := &LUT{Source: source, Max: [3]float32{1, 1, 1}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var data []float32
	line := 0
	for sc.Scan() {
		line++
		t := strings.TrimSpace(sc.Text())
		if t == "" {
			continue
		}
		if t[0] == '#' {
			if strings.HasPrefix(t, "# FBM_PLACEHOLDER") {
				l.Placeholder = true
			}
			continue
		}
		f := strings.Fields(t)
		switch strings.ToUpper(f[0]) {
		case "TITLE":
			l.Title = strings.Trim(strings.TrimSpace(t[5:]), "\"")
			continue
		case "LUT_3D_SIZE", "LUT_1D_SIZE":
			if len(f) < 2 {
				return nil, fmt.Errorf("line %d: %s without a size", line, f[0])
			}
			n, err := strconv.Atoi(f[1])
			if err != nil || n < 2 || n > 4096 {
				return nil, fmt.Errorf("line %d: bad size", line)
			}
			if strings.ToUpper(f[0]) == "LUT_3D_SIZE" {
				if n > 256 {
					return nil, fmt.Errorf("line %d: 3D size %d too large", line, n)
				}
				l.N3 = n
			} else {
				l.N1 = n
			}
			continue
		case "DOMAIN_MIN", "DOMAIN_MAX", "LUT_3D_INPUT_RANGE", "LUT_1D_INPUT_RANGE":
			var v [3]float32
			if len(f) == 4 {
				for i := 0; i < 3; i++ {
					x, _ := strconv.ParseFloat(f[i+1], 64)
					v[i] = float32(x)
				}
			} else if len(f) == 3 { // *_INPUT_RANGE min max
				a, _ := strconv.ParseFloat(f[1], 64)
				b, _ := strconv.ParseFloat(f[2], 64)
				l.Min = [3]float32{float32(a), float32(a), float32(a)}
				l.Max = [3]float32{float32(b), float32(b), float32(b)}
				continue
			}
			if strings.HasSuffix(strings.ToUpper(f[0]), "MIN") {
				l.Min = v
			} else {
				l.Max = v
			}
			continue
		}
		if len(f) != 3 {
			return nil, fmt.Errorf("line %d: expected 3 numbers", line)
		}
		for _, s := range f {
			x, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", line, err)
			}
			data = append(data, float32(x))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	need := l.N1*3 + l.N3*l.N3*l.N3*3
	if need == 0 {
		return nil, fmt.Errorf("no LUT_3D_SIZE / LUT_1D_SIZE")
	}
	if len(data) != need {
		return nil, fmt.Errorf("%d values, expected %d", len(data), need)
	}
	l.L1 = data[:l.N1*3]
	l.L3 = data[l.N1*3:]
	return l, nil
}

var lutCache sync.Map // key -> *LUT

// LoadCube loads a .cube file from disk (cached by path, size and time).
func LoadCube(path string) (*LUT, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())
	if v, ok := lutCache.Load(key); ok {
		return v.(*LUT), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	l, err := ParseCube(f, path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	lutCache.Store(key, l)
	return l, nil
}

// LoadEmbedded loads the built-in LUT for a projection white.
func LoadEmbedded(white string) (*LUT, error) {
	name := EmbeddedName(white)
	key := "embed|" + name
	if v, ok := lutCache.Load(key); ok {
		return v.(*LUT), nil
	}
	f, err := embeddedLUTs.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	l, err := ParseCube(f, "embedded "+filepath.Base(name))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	lutCache.Store(key, l)
	return l, nil
}

// LoadForNode picks the LUT of a print node: embedded (default), the
// Resolve Film Looks folder, or a custom file. A missing auto-detected or
// custom file falls back to the embedded one; note says what was used.
func LoadForNode(n Node) (l *LUT, note string, err error) {
	white := LUTWhites[clampIdx(n.Get("white"), len(LUTWhites))]
	switch clampIdx(n.Get("source"), len(LUTSources)) {
	case 1:
		if p := FindResolveLUT(white); p != "" {
			if l, err = LoadCube(p); err == nil {
				return l, "Resolve LUT " + p, nil
			}
			note = err.Error() + "; "
		} else {
			note = "Resolve " + ResolveName(white) + " not found; "
		}
	case 2:
		if p := n.Str("path"); p != "" {
			if l, err = LoadCube(p); err == nil {
				return l, "custom LUT " + p, nil
			}
			note = err.Error() + "; "
		} else {
			note = "no custom .cube chosen; "
		}
	}
	l, err = LoadEmbedded(white)
	if err != nil {
		return nil, note + "embedded LUT missing", err
	}
	note += "embedded " + filepath.Base(EmbeddedName(white))
	if l.Placeholder {
		note += " (PLACEHOLDER test LUT, not the real 2383)"
	}
	return l, note, nil
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }

func (l *LUT) apply1D(c int, v float32) float32 {
	n := l.N1
	x := (v - l.Min[c]) / (l.Max[c] - l.Min[c]) * float32(n-1)
	if !(x > 0) {
		return l.L1[c]
	}
	if x >= float32(n-1) {
		return l.L1[(n-1)*3+c]
	}
	i := int(x)
	t := x - float32(i)
	return lerp(l.L1[i*3+c], l.L1[(i+1)*3+c], t)
}

// Apply maps one RGB triple (input clamped to the domain like Resolve).
func (l *LUT) Apply(r, g, b float32, tetra bool) (float32, float32, float32) {
	if l.N1 > 0 {
		r, g, b = l.apply1D(0, r), l.apply1D(1, g), l.apply1D(2, b)
		if l.N3 == 0 {
			return r, g, b
		}
	}
	n := l.N3
	pos := func(v float32, c int) (int, float32) {
		x := (v - l.Min[c]) / (l.Max[c] - l.Min[c]) * float32(n-1)
		if !(x > 0) {
			return 0, 0
		}
		if x >= float32(n-1) {
			return n - 2, 1
		}
		i := int(x)
		return i, x - float32(i)
	}
	ir, fr := pos(r, 0)
	ig, fg := pos(g, 1)
	ib, fb := pos(b, 2)
	at := func(dr, dg, db int) (float32, float32, float32) {
		o := (((ib+db)*n+(ig+dg))*n + (ir + dr)) * 3
		return l.L3[o], l.L3[o+1], l.L3[o+2]
	}
	if tetra {
		c000r, c000g, c000b := at(0, 0, 0)
		c111r, c111g, c111b := at(1, 1, 1)
		var ar, ag, ab, br, bg, bb float32 // the two middle vertices
		var w1, w2, w3 float32
		switch {
		case fr >= fg && fg >= fb:
			ar, ag, ab = at(1, 0, 0)
			br, bg, bb = at(1, 1, 0)
			w1, w2, w3 = fr, fg, fb
		case fr >= fb && fb >= fg:
			ar, ag, ab = at(1, 0, 0)
			br, bg, bb = at(1, 0, 1)
			w1, w2, w3 = fr, fb, fg
		case fb >= fr && fr >= fg:
			ar, ag, ab = at(0, 0, 1)
			br, bg, bb = at(1, 0, 1)
			w1, w2, w3 = fb, fr, fg
		case fg >= fr && fr >= fb:
			ar, ag, ab = at(0, 1, 0)
			br, bg, bb = at(1, 1, 0)
			w1, w2, w3 = fg, fr, fb
		case fg >= fb && fb >= fr:
			ar, ag, ab = at(0, 1, 0)
			br, bg, bb = at(0, 1, 1)
			w1, w2, w3 = fg, fb, fr
		default: // fb >= fg >= fr
			ar, ag, ab = at(0, 0, 1)
			br, bg, bb = at(0, 1, 1)
			w1, w2, w3 = fb, fg, fr
		}
		mix := func(c0, ca, cb, c1 float32) float32 {
			return (1-w1)*c0 + (w1-w2)*ca + (w2-w3)*cb + w3*c1
		}
		return mix(c000r, ar, br, c111r), mix(c000g, ag, bg, c111g), mix(c000b, ab, bb, c111b)
	}
	var out [3]float32
	for c := 0; c < 3; c++ {
		v := func(dr, dg, db int) float32 {
			o := (((ib+db)*n+(ig+dg))*n + (ir + dr)) * 3
			return l.L3[o+c]
		}
		x00 := lerp(v(0, 0, 0), v(1, 0, 0), fr)
		x10 := lerp(v(0, 1, 0), v(1, 1, 0), fr)
		x01 := lerp(v(0, 0, 1), v(1, 0, 1), fr)
		x11 := lerp(v(0, 1, 1), v(1, 1, 1), fr)
		out[c] = lerp(lerp(x00, x10, fg), lerp(x01, x11, fg), fb)
	}
	return out[0], out[1], out[2]
}
