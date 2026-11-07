package hdrscan

import (
	"basetool/rawconv"
	"basetool/scanmeta"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
)

func TestScanMetadataUsesLastLevelingNotScene(t *testing.T) {
	const w, h = 96, 96
	scene := [3]float64{.1330281528953994, .055115587090867474, .038910505836575876}
	accepted := [3]float64{.49844, .49639, .50210}
	values := [3]int{24, 73, 43}
	matrix := [9]float64{.8643, .1047, -.0197, -.0507, .9818, .0164, .1129, -.1462, 1.0409}
	l := &scanmeta.Leveling{Base: accepted, Values: values, At: "2026-11-07T22:57:46+08:00"}
	dir := t.TempDir()
	var paths [3][]string
	for ch := 0; ch < 3; ch++ {
		for frame := 0; frame < 2; frame++ {
			pixels := make([]uint16, w*h*3)
			for i := range pixels {
				v := scene[i%3]
				if i%3 != ch {
					v /= 20
				}
				if frame == 1 {
					v *= 4
				}
				pixels[i] = uint16(v * 65535)
			}
			path := filepath.Join(dir, fmt.Sprintf("%d-%d.tiff", ch, frame))
			if err := rawconv.WriteTIFF16(path, w, h, pixels, false, "test", "test", "test"); err != nil {
				t.Fatal(err)
			}
			paths[ch] = append(paths[ch], path)
		}
	}
	for _, tri := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, dust := range []int{DustOff, DustLight} {
				name := fmt.Sprintf("tri=%v/enabled=%v/dust=%d", tri, enabled, dust)
				t.Run(name, func(t *testing.T) {
					saved := l
					if !enabled {
						saved = nil
					}
					m := (scanmeta.Metadata{LightRGB: &values, CameraMatrix: &matrix, CameraStrength: 1}).WithLeveling(saved, values)
					o := ProcessOptions{OutDir: dir, Merge: DefaultOptions(), BaseArea: image.Rect(0, 0, w, h), Metadata: m, Dust: dust}
					var path string
					if tri {
						res, err := ProcessTrichrome(paths, []float64{1, 4}, o)
						if err != nil {
							t.Fatal(err)
						}
						path = res.Out
						if res.BaseN != 0 {
							t.Fatal("normal capture still measures a post-merge base")
						}
					} else {
						o.Merge.Exposure = []float64{1, 4}
						res, err := ProcessN(paths[0], o)
						if err != nil {
							t.Fatal(err)
						}
						path = res.HDR
						if res.Base1N != 0 {
							t.Fatal("normal white capture still measures a post-merge base")
						}
					}
					info, err := ReadTIFFInfo(path)
					if err != nil {
						t.Fatal(err)
					}
					var got scanmeta.Metadata
					if err = json.Unmarshal([]byte(info.Description), &got); err != nil {
						t.Fatal(err)
					}
					if enabled {
						if got.FilmBase == nil || *got.FilmBase != accepted || got.BaseSource != scanmeta.LevelingSource || got.FilmBaseSpace != scanmeta.AfterProfile {
							t.Fatalf("scene values replaced leveling: %+v", got)
						}
					} else if got.FilmBase != nil || got.FilmBaseSpace != "" {
						t.Fatalf("missing leveling fell back to image: %+v", got)
					}
					if got.CameraApplied || got.CameraMatrix == nil || *got.CameraMatrix != matrix || got.LightRGB == nil || *got.LightRGB != values {
						t.Fatal("capture provenance changed")
					}
					if prefix := os.Getenv("NSS_LEVELING_FIXTURE"); prefix != "" && enabled && dust == DustLight {
						data, err := os.ReadFile(path)
						if err == nil {
							err = os.WriteFile(fmt.Sprintf("%s-%v.tiff", prefix, tri), data, 0644)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					t.Logf("accepted leveling=%v; unrelated scene=%v; metadata=%v", accepted, scene, got.FilmBase)
				})
			}
		}
	}
}
