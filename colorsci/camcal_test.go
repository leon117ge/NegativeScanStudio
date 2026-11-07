package colorsci_test

import (
	"os"
	"testing"

	"basetool/colorsci"
	"basetool/hdrscan"
)

func TestCamCalChartcal(t *testing.T) {
	tif := os.Getenv("CAMCAL_TIFF")
	if tif == "" {
		t.Skip("set CAMCAL_TIFF and CAMCAL_REF")
	}
	rf, err := os.Open(os.Getenv("CAMCAL_REF"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := colorsci.ParseCGATS(rf)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := hdrscan.Load(tif)
	if err != nil {
		t.Fatal(err)
	}
	c, orient, err := colorsci.DetectCamChart(pl.Pix, pl.W, pl.H)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("orientation %s corners %v", orient, c)
	ps := colorsci.SampleCamChart(pl.Pix, pl.W, pl.H, c, 0.25)
	for i, p := range ps {
		t.Logf("%s %.5f %.5f %.5f clip %.4f %.4f %.4f %v", colorsci.CamPatchNames[i], p.Med[0], p.Med[1], p.Med[2], p.ClipFrac[0], p.ClipFrac[1], p.ClipFrac[2], p.Clipped)
	}
	f, err := colorsci.FitCamChart(ps, ref)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("used %d clipped %v clipEV %.2f", f.Used, f.Clipped, f.ClipEV)
	t.Logf("before mean %.3f max %.3f  after mean %.3f max %.3f", f.MeanB, f.MaxB, f.MeanA, f.MaxA)
	t.Logf("Mx %v", f.Mx)
	t.Logf("Neutral %v", f.Neutral)
	t.Logf("BaseMul %v grey %v", f.BaseMul, f.Grey)
	for i := range ps {
		t.Logf("%s %v %.2f %.2f", colorsci.CamPatchNames[i], f.Use[i], f.DEBefore[i], f.DEAfter[i])
	}
}
