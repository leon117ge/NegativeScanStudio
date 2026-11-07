package camera

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sync"

	"basetool/rawconv"
)

// Sim is a simulated camera. It "photographs" a real RAW file: every capture
// rewrites the sensor data of the source file scaled by the exposure ratio
// (clipped at the white level), so the result is a genuine RAF that LibRaw
// decodes exactly like a camera file.
type Sim struct {
	Mode  string // "" or "M" = manual; "A", "P", "S" simulate a body not in M
	mu    sync.Mutex
	src   string
	file  []byte
	frame *rawconv.RawFrame
	off   int
	exp   Exposure
	// Native is the exposure at which the source file's values are reproduced 1:1.
	Native Exposure
	// LongShutterError multiplies the true exposure for shutters of 1/30 s or
	// longer (simulates real shutter timing differences, e.g. 1.012).
	LongShutterError float64
	// DialsOnly simulates a body that refuses PTP exposure writes.
	DialsOnly bool
	// ChannelGain (v0.81), if set, scales each CFA colour (R, G, B) of a
	// capture: the simulated RGB light's lit channels (trichrome tests).
	ChannelGain func() [3]float64
	log       *Logger
	n         int
	lvW, lvH  int
	lvRGB     []float32
}

// OpenSim loads a RAW file as the simulated scene.
func OpenSim(src string, log *Logger) (*Sim, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	f, err := rawconv.ReadRawFrame(src)
	if err != nil {
		return nil, err
	}
	// Locate the uncompressed 16-bit sensor data inside the file.
	pat := make([]byte, 128)
	for i := 0; i < 64; i++ {
		binary.LittleEndian.PutUint16(pat[2*i:], f.Raw[i])
	}
	off := bytes.Index(data, pat)
	if off < 0 || off+2*len(f.Raw) > len(data) {
		return nil, errors.New("simulated camera needs an uncompressed RAW (sensor data not found in file)")
	}
	for i := 0; i < len(f.Raw); i += 4099 {
		if binary.LittleEndian.Uint16(data[off+2*i:]) != f.Raw[i] {
			return nil, errors.New("simulated camera: sensor data layout not recognised")
		}
	}
	s := &Sim{src: src, file: data, frame: f, off: off, log: log,
		Native: Exposure{ISO: 160, Shutter: 7812}, LongShutterError: 1.012}
	s.exp = Exposure{ISO: 160, Shutter: 7812}
	s.lvW, s.lvH, s.lvRGB, _ = f.Binned(8)
	log.Printf("Simulated camera: scene %s (%s %s, %dx%d, black %d, white %d, sensor data at offset %d)",
		filepath.Base(src), f.Make, f.Model, f.Width, f.Height, f.Black, f.Maximum, off)
	return s, nil
}

func (s *Sim) gain(e Exposure) float64 {
	g := float64(e.ISO) / float64(s.Native.ISO) * float64(e.Shutter) / float64(s.Native.Shutter)
	if e.Shutter >= 31250 && s.LongShutterError > 0 {
		g *= s.LongShutterError
	}
	return g
}

func (s *Sim) Info() Info {
	return Info{Driver: "Simulated", Manufacturer: "Simulated", Model: "Camera (" + s.frame.Model + " scene)", Version: "sim",
		VendorExt: VendorFuji, VendorDesc: "simulation", Ops: []uint16{OpGetDeviceInfo, OpGetObjectHandles, OpGetObjectInfo, OpGetObject,
			OpDeleteObject, OpInitiateCapture, OpGetDevicePropDesc, OpGetDevicePropValue, OpSetDevicePropValue, OpInitiateOpenCapture, OpTerminateOpenCapture},
		Props: []uint16{PropExposureTime, PropExposureIndex, PropFNumber, PropFujiPriority, PropFujiCurrentState}}
}

func (s *Sim) Exposure() (Exposure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exp, nil
}

// ExposureMode: the simulated body is in M unless Mode names another mode.
func (s *Sim) ExposureMode() (bool, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mode == "" || s.Mode == "M" {
		return true, "M", nil
	}
	return false, s.Mode, nil
}

func (s *Sim) SetExposure(e Exposure) (Exposure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mode != "" && s.Mode != "M" {
		return s.exp, fmt.Errorf("%w - the camera is in %s (simulated)", ErrNotManual, s.Mode)
	}
	if s.DialsOnly {
		return s.exp, fmt.Errorf("%w (simulated dial-only body)", ErrNotWritable)
	}
	ok := false
	for _, i := range ISOs {
		ok = ok || i == e.ISO
	}
	if !ok || Shutters[ShutterIndex(e.Shutter)].Value != e.Shutter {
		return s.exp, fmt.Errorf("invalid exposure %s", e)
	}
	s.exp = e
	s.log.Printf("SIM set exposure %s", e)
	return e, nil
}

// SetDials changes the exposure as if the user turned the dials.
func (s *Sim) SetDials(e Exposure) {
	s.mu.Lock()
	s.exp = e
	s.mu.Unlock()
}

func (s *Sim) LiveView() ([]byte, error) {
	s.mu.Lock()
	g := s.gain(s.exp)
	s.mu.Unlock()
	w, h := s.lvW, s.lvH
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	lut := make([]uint8, 4097)
	for i := range lut {
		lut[i] = uint8(255*math.Pow(float64(i)/4096, 1/2.2) + 0.5)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			var c [3]uint8
			for k := 0; k < 3; k++ {
				v := float64(s.lvRGB[i+k]) * g
				if v > 1 {
					v = 1
				}
				c[k] = lut[int(v*4096)]
			}
			img.SetRGBA(x, y, color.RGBA{c[0], c[1], c[2], 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (s *Sim) StopLiveView() error { return nil }

func (s *Sim) Capture(dir, prefix string) (string, error) {
	s.mu.Lock()
	e := s.exp
	g := s.gain(e)
	s.n++
	n := s.n
	s.mu.Unlock()
	out := make([]byte, len(s.file))
	copy(out, s.file)
	f := s.frame
	white := float64(f.Maximum)
	cg := [3]float64{1, 1, 1}
	if s.ChannelGain != nil {
		cg = s.ChannelGain()
	}
	chg := cg != [3]float64{1, 1, 1}
	// Scale every visible sensor pixel around its own black level.
	for y := 0; y < f.RawHeight; y++ {
		vy := y - f.Top
		for x := 0; x < f.RawWidth; x++ {
			vx := x - f.Left
			if vy < 0 || vy >= f.Height || vx < 0 || vx >= f.Width {
				continue
			}
			i := y*f.RawWidth + x
			b := float64(f.BlackAt[vy*f.Width+vx])
			gg := g
			if chg {
				c := f.Color[vy*f.Width+vx]
				if c == 3 {
					c = 1
				}
				if c < 3 {
					gg *= cg[c]
				}
			}
			v := b + (float64(f.Raw[i])-b)*gg
			if v > white {
				v = white
			}
			if v < 0 {
				v = 0
			}
			binary.LittleEndian.PutUint16(out[s.off+2*i:], uint16(v+0.5))
		}
	}
	name := fmt.Sprintf("%sSIM_%04d.RAF", prefix, n)
	dst := uniquePath(filepath.Join(dir, name))
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return "", err
	}
	if chg {
		s.log.Printf("SIM captured %s at %s (gain %.4f vs scene, light R %.2f G %.2f B %.2f), downloaded, deleted from camera", filepath.Base(dst), e, g, cg[0], cg[1], cg[2])
	} else {
		s.log.Printf("SIM captured %s at %s (gain %.4f vs scene), downloaded, deleted from camera", filepath.Base(dst), e, g)
	}
	return dst, nil
}

func (s *Sim) Diagnostics() error {
	i := s.Info()
	s.log.Printf("==== Camera diagnostics (simulated) ====")
	s.log.Printf("Scene file: %s", s.src)
	s.log.Printf("Model: %s | ops: %s | props: %s", i.Model, hexList(i.Ops), hexList(i.Props))
	s.log.Printf("Native exposure (1:1 with scene file): %s; long-shutter error ×%.3f; dials only: %v", s.Native, s.LongShutterError, s.DialsOnly)
	s.log.Printf("==== end of diagnostics ====")
	return nil
}

func (s *Sim) Close() error {
	s.log.Printf("Simulated camera closed")
	return nil
}

// Status implements StatusReader (the simulated lens is at f/5.6).
func (s *Sim) Status() (Exposure, float64, error) {
	e, err := s.Exposure()
	return e, 5.6, err
}
