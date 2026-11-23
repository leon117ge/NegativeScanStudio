package hdrscan

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// grayToRGB replicates a monochrome plane as R=G=B so ProcessN / Load can
// reuse the existing HDR merge path.
func grayToRGB(p MonoPlane) []uint16 {
	out := make([]uint16, 3*p.W*p.H)
	for i := 0; i < p.W*p.H && i < len(p.Pix); i++ {
		v := p.Pix[i]
		out[3*i+0], out[3*i+1], out[3*i+2] = v, v, v
	}
	return out
}

// mergeMonoBracketRGB is the v1.00-v1.09 mono HDR path, kept only so
// tests can compare the v1.10 native merge against it. It merges a per-channel exposure bracket the same way
// white-light and trichrome HDR do: write each mono frame as a 16-bit RGB
// TIFF (gray replicated) and call ProcessN with the bracket exposure
// ratios. The merged HDR is read back as one linear MonoPlane.
func mergeMonoBracketRGB(frames []MonoPlane, evs []int, o ProcessOptions) (MonoPlane, error) {
	if len(frames) == 0 {
		return MonoPlane{}, fmt.Errorf("no bracket frames")
	}
	if len(frames) == 1 {
		return frames[0], nil
	}
	for i, fr := range frames {
		if err := checkMono(fr, fmt.Sprintf("bracket %d", i)); err != nil {
			return MonoPlane{}, err
		}
		if fr.W != frames[0].W || fr.H != frames[0].H {
			return MonoPlane{}, fmt.Errorf("bracket sizes differ: 0 is %dx%d, %d is %dx%d", frames[0].W, frames[0].H, i, fr.W, fr.H)
		}
	}
	work, err := os.MkdirTemp("", "nss-mono-hdr-*")
	if err != nil {
		return MonoPlane{}, err
	}
	defer os.RemoveAll(work)
	keepDir := ""
	if o.KeepTIFF && o.OutDir != "" {
		keepDir = o.OutDir
	}
	paths := make([]string, len(frames))
	exps := make([]float64, len(frames))
	meta := o.Metadata
	meta.Mode = "mono-bracket-component"
	meta.Encoding = "linear"
	for i, fr := range frames {
		ev := 0
		if i < len(evs) {
			ev = evs[i]
		}
		name := fmt.Sprintf("mono_br_%d_%s.tiff", i, StepTag(ev))
		path := filepath.Join(work, name)
		if keepDir != "" {
			path = uniqueFile(filepath.Join(keepDir, name))
		}
		sw := o.Software
		if sw == "" {
			sw = "NegativeScan Studio"
		}
		if err := writeScanTIFF(path, fr.W, fr.H, grayToRGB(fr), false, "Hikrobot", "mono", sw+" mono HDR bracket frame", meta); err != nil {
			return MonoPlane{}, err
		}
		paths[i] = path
		exps[i] = math.Pow(2, float64(ev))
	}
	co := o
	co.OutDir = work
	co.KeepTIFF = false
	co.DeleteRAW = false
	co.Merge.Exposure = exps
	co.Metadata = meta
	if o.Progress != nil {
		o.Progress(fmt.Sprintf("Merging mono HDR bracket (%d frames) ...", len(frames)))
	}
	res, err := ProcessN(paths, co)
	if err != nil {
		return MonoPlane{}, err
	}
	p, err := Load(res.HDR)
	if err != nil {
		return MonoPlane{}, err
	}
	defer p.Free()
	n := p.W * p.H
	gray := make([]uint16, n)
	for i := 0; i < n; i++ {
		gray[i] = p.Pix[3*i+1]
	}
	return MonoPlane{W: p.W, H: p.H, Pix: gray}, nil
}
