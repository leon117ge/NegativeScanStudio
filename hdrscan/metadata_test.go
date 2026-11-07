package hdrscan

import (
	"basetool/cropper"
	"basetool/rawconv"
	"basetool/scanmeta"
	"encoding/json"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestScanTIFFMetadataAndHairPreservation(t *testing.T) {
	b := [3]float64{.48, .50, .46}
	light := [3]int{1200, 2300, 3400}
	matrix := [9]float64{1.1, -.1, 0, 0, 1, 0, 0, 0, 1}
	meta := scanmeta.Metadata{Roll: "\u6d4b\u8bd5\u5377📷", LightRGB: &light, LightModel: "CuteNewDesign RGB Light Air", FilmBase: &b, CameraMatrix: &matrix, CameraStrength: .5}
	for _, lzw := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "scan.tiff")
		pix := make([]uint16, 24*24*3)
		for i := range pix {
			pix[i] = uint16(1000 + i)
		}
		if e := writeScanTIFF(path, 24, 24, pix, lzw, "FUJIFILM", "X-T3", "NegativeScan Studio v0.90", meta); e != nil {
			t.Fatal(e)
		}
		ti, e := ReadTIFFInfo(path)
		if e != nil {
			t.Fatal(e)
		}
		var got scanmeta.Metadata
		if e = json.Unmarshal([]byte(ti.Description), &got); e != nil {
			t.Fatal(e)
		}
		if got.Roll != meta.Roll || got.Schema != scanmeta.Schema || got.FilmBase == nil || *got.FilmBase != b || got.LightRGB == nil || *got.LightRGB != light || got.CameraApplied || ti.LZW != lzw {
			t.Fatalf("bad metadata: %+v", got)
		}
		if e := SaveHairTIFF(path, 24, 24, pix, ti); e != nil {
			t.Fatal(e)
		}
		ti2, e := ReadTIFFInfo(path)
		if e != nil || ti2.Description != ti.Description {
			t.Fatal("description lost on hair rewrite", e)
		}
		if fixture := os.Getenv("NSS_META_FIXTURE"); fixture != "" && !lzw {
			data, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(fixture, data, 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestTrichromePipelineWritesMetadata(t *testing.T) {
	dir := t.TempDir()
	var paths [3][]string
	light := [3]int{10, 20, 30}
	for ch := 0; ch < 3; ch++ {
		path := filepath.Join(dir, string(rune('R'+ch))+".tiff")
		pix := make([]uint16, 24*24*3)
		for i := range pix {
			pix[i] = uint16(28000 + ch*1000)
		}
		if err := rawconv.WriteTIFF16(path, 24, 24, pix, false, "test", "test", "test"); err != nil {
			t.Fatal(err)
		}
		paths[ch] = []string{path}
	}
	r, err := ProcessTrichrome(paths, []float64{1}, ProcessOptions{Merge: DefaultOptions(), OutDir: dir, Software: "NSS v0.90 test", BaseArea: image.Rect(2, 2, 22, 22), Metadata: scanmeta.Metadata{LightRGB: &light, LightModel: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	ti, err := ReadTIFFInfo(r.Out)
	if err != nil {
		t.Fatal(err)
	}
	var m scanmeta.Metadata
	if err = json.Unmarshal([]byte(ti.Description), &m); err != nil {
		t.Fatal(err)
	}
	if m.FilmBase == nil || m.Mode != "trichrome" || m.LightRGB == nil || *m.LightRGB != light {
		t.Fatalf("metadata %+v", m)
	}
	for c := 0; c < 3; c++ {
		want := float64(28000+c*1000) / 65535
		if math.Abs((*m.FilmBase)[c]-want) > 1e-6 {
			t.Fatalf("base %v want %.6f", m.FilmBase, want)
		}
	}
	im, err := cropper.Load(r.Out)
	if err != nil {
		t.Fatal(err)
	}
	cropped := filepath.Join(dir, "cropped.tiff")
	if err = cropper.Save(cropped, im, cropper.SaveOptions{LZW: true, Software: "NSS crop test"}); err != nil {
		t.Fatal(err)
	}
	after, err := ReadTIFFInfo(cropped)
	if err != nil || after.Description != ti.Description {
		t.Fatal("crop dropped metadata", err)
	}
}
