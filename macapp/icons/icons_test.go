package icons

import (
	"image/color"
	"testing"

	"basetool/macapp/internal/drift"
)

func TestIconsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "icons.go")
	pages := drift.Decls(t, "../../pages.go")
	card := drift.Decls(t, "../../card.go")
	drift.Same(t, ours, pages, "vRect", "vArc", "iconFilm", "iconWB", "iconHDR", "iconCrop", "iconDevelop", "iconLight", "iconTri")
	drift.Same(t, ours, card, "iconCamera", "iconMeter", "iconPreset")
}

func TestRenderDrawsSomething(t *testing.T) {
	for name := range ByName {
		img := Render(name, 24, color.NRGBA{232, 124, 44, 255})
		n := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				n++
			}
		}
		if n < 20 || n > 24*24-20 {
			t.Errorf("%s: %d covered pixels", name, n)
		}
	}
}
