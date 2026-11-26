package appstate

import (
	"image"

	"basetool/hdrscan"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"basetool/macapp/internal/drift"
)

func TestCopiedDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "hdrconfig.go")
	drift.Same(t, ours, drift.Decls(t, "../../hdrpage.go"),
		"appName", "appVersion", "HDRConfig", "bracketEVs", "bracketItems", "bracketLegacy", "bracketDefault",
		"dustItems", "dustKeys", "frame0Items", "frame0Keys", "isoVals", "isoItems", "stockTolVals", "stockTolItems",
		"stockTolDefault", "evKey", "evText", "slowestVals", "slowestItems", "targetVals", "targetItems", "targetDefault",
		"targetValsV04", "targetIndex", "alignItems", "alignModes", "mergeItems", "mergeModes", "mergeKeys", "mergeDefault",
		"mergeTip", "hdrConfigFile", "defaultOutDir", "loadHDRConfig", "keyIndex", "saveHDRConfig")
	drift.Same(t, ours, drift.Decls(t, "../../tripage.go"), "fixedShutterVals", "fixedShutterItems", "fixedShutterKeys", "fixedShutterTip", "fixedShutterIndex", "triModeKeys")
	drift.Same(t, ours, drift.Decls(t, "../../monoopts_ui.go"), "monoBracketEVs", "monoBracketItems", "monoBracketDefault", "monoBracketIndex", "framesPerExpMin", "framesPerExpMax")
	drift.Same(t, ours, drift.Decls(t, "../../lightpage.go"), "clampIdx")
	drift.Same(t, ours, drift.Decls(t, "../../presets.go"), "configDir")
}

// The combo items written inline in hdrpage.go's layout must match ours.
func TestInlineItemsMatchWindows(t *testing.T) {
	b, err := os.ReadFile("../../hdrpage.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, l := range [][]string{AreaItems, TIFFItems, RAFItems, CompItems} {
		lit := `[]string{"` + strings.Join(l, `", "`) + `"}`
		if !strings.Contains(src, lit) {
			t.Errorf("%s not in hdrpage.go", lit)
		}
	}
	if !strings.Contains(src, `const simItem = "`+SimItem+`"`) {
		t.Error("simItem differs")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	c := LoadHDRConfig()
	if c.Target != targetDefault || !c.LZW || c.Merge != mergeDefault || !c.CenterMark {
		t.Fatalf("defaults: %+v", c)
	}
	c.Roll, c.Bracket, c.Frame0, c.ISO, c.Dust, c.OutDir = "Portra400_Kyoto", 1, 1, 2, 2, "/tmp/scans"
	SaveHDRConfig(c)
	d := LoadHDRConfig()
	if d.Roll != c.Roll || d.Bracket != 1 || d.Frame0 != 1 || FixedISO(d) != 250 || d.Dust != 2 || d.OutDir != "/tmp/scans" {
		t.Fatalf("round trip: %+v", d)
	}
	if got := EVText(EVs(d)); got != "0 / +2 / +3 EV" {
		t.Fatalf("EVText = %q", got)
	}
	b, _ := os.ReadFile(filepath.Join(ConfigDir(), "hdr_config.txt"))
	if !strings.Contains(string(b), "BRACKET_EV=0,2,3\r\n") || !strings.Contains(string(b), "FRAME0=fixed\r\n") {
		t.Fatalf("file:\n%s", b)
	}
}

func TestNavMatchesWindows(t *testing.T) {
	b, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, m := range Modules {
		if !strings.Contains(src, `"`+m.Title+`"`) {
			t.Errorf("module %q not in main.go", m.Title)
		}
		for _, s := range m.Subs {
			if !strings.Contains(src, `navItem{label: "`+s+`"}`) {
				t.Errorf("sub-page %q not in main.go", s)
			}
		}
	}
	if len(AllPages()) != 11 {
		t.Fatalf("%d pages", len(AllPages()))
	}
}

func TestLiveViewRouting(t *testing.T) {
	cases := []struct {
		m, s, want int
		page       Page
	}{
		{0, 0, 0, PageCamera}, {0, 1, 3, PageLight}, {1, 0, 1, PageWhite}, {1, 1, 2, PageTri},
		{1, 2, -1, PageMono}, {1, 3, -1, PageFocus}, {2, 1, -1, PageMerge}, {3, 0, -1, PageConvert},
	}
	for _, c := range cases {
		var n Nav
		n.SetModule(c.m)
		n.SetSub(c.s)
		if n.LiveView() != c.want || n.Page() != c.page {
			t.Errorf("module %d sub %d: live %d page %s", c.m, c.s, n.LiveView(), n.Page())
		}
	}
	var n Nav
	n.SetModule(1)
	n.SetSub(2)
	n.SetModule(0)
	n.SetModule(1)
	if n.Page() != PageMono {
		t.Fatal("sub-page not kept per module")
	}
	if n.SetSub(9) || n.SetModule(7) {
		t.Fatal("out of range accepted")
	}
}

func TestFromArgs(t *testing.T) {
	n, files := FromArgs([]string{"--crop", "a.tif", "b.tif"})
	if n.Page() != PageCrop || len(files) != 2 {
		t.Fatal(n.Page(), files)
	}
	for arg, p := range map[string]Page{"--light": PageLight, "--tri": PageTri, "--mono": PageMono, "--focus": PageFocus,
		"--merge": PageMerge, "--hairs": PageHairs, "--batch": PageBatch, "--convert": PageConvert, "--white": PageWhite} {
		if n, _ := FromArgs([]string{arg}); n.Page() != p {
			t.Errorf("%s -> %s", arg, n.Page())
		}
	}
}

func TestGeometry(t *testing.T) {
	ir := FitRect(image.Rect(0, 0, 600, 300), image.Pt(600, 400))
	if ir != image.Rect(75, 0, 525, 300) {
		t.Fatal(ir)
	}
	x, y := PxToNorm(ir, image.Pt(300, 150))
	if x != 0.5 || y != 0.5 {
		t.Fatal(x, y)
	}
	if x, _ := PxToNorm(ir, image.Pt(0, 0)); x != 0 {
		t.Fatal("not clamped")
	}
	r := DragRect(0.6, 0.6, 0.2, 0.4)
	if r.X0 != 0.2 || r.Y1 != 0.6 {
		t.Fatal(r)
	}
	px := NormToPx(ir, hdrscan.Rect{X0: 0.5, Y0: 0.5, X1: 0.5, Y1: 0.5})
	if px.Dx() != 6 || px.Dy() != 6 {
		t.Fatal(px)
	}
}
