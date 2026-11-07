package develop

import (
	"math"
	"testing"
)

// reference ops on a w x h single-value-per-pixel grid
func refOp(g [][]int, op int) [][]int {
	h, w := len(g), len(g[0])
	var out [][]int
	mk := func(W, H int) {
		out = make([][]int, H)
		for i := range out {
			out[i] = make([]int, W)
		}
	}
	switch op {
	case OpRotRight: // pixel at (x,y) goes to (h-1-y, x)
		mk(h, w)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out[x][h-1-y] = g[y][x]
			}
		}
	case OpRotLeft:
		mk(h, w)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out[w-1-x][y] = g[y][x]
			}
		}
	case OpRot180:
		mk(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out[h-1-y][w-1-x] = g[y][x]
			}
		}
	case OpFlipH:
		mk(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out[y][w-1-x] = g[y][x]
			}
		}
	case OpFlipV:
		mk(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out[h-1-y][x] = g[y][x]
			}
		}
	}
	return out
}

func gridPix(w, h int) ([]uint16, [][]int) {
	pix := make([]uint16, w*h*3)
	g := make([][]int, h)
	for y := 0; y < h; y++ {
		g[y] = make([]int, w)
		for x := 0; x < w; x++ {
			v := y*w + x
			g[y][x] = v
			pix[v*3], pix[v*3+1], pix[v*3+2] = uint16(v), uint16(v+20000), uint16(v+40000)
		}
	}
	return pix, g
}

// Every sequence of up to 4 operations equals the composed orientation,
// applied once to the original pixels (odd and even sizes).
func TestOrientMatchesReference(t *testing.T) {
	for _, sz := range [][2]int{{5, 3}, {4, 6}, {7, 7}, {1, 4}} {
		w, h := sz[0], sz[1]
		pix, g := gridPix(w, h)
		var walk func(o Orient, ref [][]int, depth int)
		walk = func(o Orient, ref [][]int, depth int) {
			out, W, H := OrientU16(pix, w, h, o)
			if H != len(ref) || W != len(ref[0]) {
				t.Fatalf("%dx%d o=%d dims %dx%d want %dx%d", w, h, o, W, H, len(ref[0]), len(ref))
			}
			for y := 0; y < H; y++ {
				for x := 0; x < W; x++ {
					v := ref[y][x]
					d := (y*W + x) * 3
					if int(out[d]) != v || int(out[d+1]) != v+20000 || int(out[d+2]) != v+40000 {
						t.Fatalf("%dx%d o=%d (%s) at %d,%d got %d want %d", w, h, o, o, x, y, out[d], v)
					}
				}
			}
			if depth == 4 {
				return
			}
			for op := OpRotLeft; op <= OpFlipV; op++ {
				walk(o.Apply(op), refOp(ref, op), depth+1)
			}
		}
		walk(0, g, 0)
	}
}

func TestOrientGroup(t *testing.T) {
	seen := map[Orient]bool{}
	for o := Orient(0); o < 8; o++ {
		seen[o.Apply(OpRotLeft).Apply(OpRotRight)] = true
		if o.Apply(OpRotLeft).Apply(OpRotRight) != o || o.Apply(OpFlipH).Apply(OpFlipH) != o || o.Apply(OpRot180).Apply(OpRot180) != o {
			t.Fatalf("inverse failed for %d", o)
		}
		if o.Apply(OpFlipV) != o.Apply(OpFlipH).Apply(OpRot180) {
			t.Fatalf("flipV != flipH+180 for %d", o)
		}
	}
	if len(seen) != 8 || Orient(0).String() != "as scanned" {
		t.Fatal("group")
	}
}

// Continuous mapping agrees with the pixel remap (pixel centres) and
// round-trips.
func TestOrientPointMapping(t *testing.T) {
	w, h := 9, 4
	for o := Orient(0); o < 8; o++ {
		W, H := o.Dims(w, h)
		for Y := 0; Y < H; Y++ {
			for X := 0; X < W; X++ {
				sx, sy := o.Src(w, h, X, Y)
				fx, fy := o.ToSrcF(w, h, float64(X)+0.5, float64(Y)+0.5)
				if math.Abs(fx-float64(sx)-0.5) > 1e-9 || math.Abs(fy-float64(sy)-0.5) > 1e-9 {
					t.Fatalf("o=%d %d,%d: int %d,%d float %.2f,%.2f", o, X, Y, sx, sy, fx, fy)
				}
				bx, by := o.FromSrcF(w, h, fx, fy)
				if math.Abs(bx-float64(X)-0.5) > 1e-9 || math.Abs(by-float64(Y)-0.5) > 1e-9 {
					t.Fatalf("round trip o=%d", o)
				}
			}
		}
	}
}

func TestRegionF32(t *testing.T) {
	w, h := 12, 8
	pix, _ := gridPix(w, h)
	for o := Orient(0); o < 8; o++ {
		full, W, _ := OrientU16(pix, w, h, o)
		r := RegionF32(pix, w, w, h, o, 2, 2, 2, 3, 2) // 2x2 blocks from (2,2)
		for j := 0; j < 3; j++ {
			for i := 0; i < 2; i++ {
				var s float64
				for yy := 0; yy < 2; yy++ {
					for xx := 0; xx < 2; xx++ {
						s += float64(full[((2+j*2+yy)*W+2+i*2+xx)*3])
					}
				}
				if math.Abs(float64(r[(j*2+i)*3])-s/4/65535) > 1e-6 {
					t.Fatalf("o=%d region %d,%d", o, i, j)
				}
			}
		}
	}
}

func TestViewZoomAndPick(t *testing.T) {
	v := View{Fit: true, IW: 6240, IH: 4160, VX: 0, VY: 0, VW: 1400, VH: 900, Margin: 8}
	fs := v.FitScale()
	if math.Abs(fs-math.Min(1384.0/6240, 884.0/4160)) > 1e-12 || v.Label() != "Fit 21%" {
		t.Fatalf("fit %.4f %s", fs, v.Label())
	}
	// image point under the cursor stays put when zooming
	ix, iy := v.ToImage(300, 200)
	v.ZoomAt(1, 300, 200)
	jx, jy := v.ToImage(300, 200)
	if math.Abs(ix-jx) > 1e-9 || math.Abs(iy-jy) > 1e-9 || v.Label() != "100%" {
		t.Fatalf("zoom anchor %f,%f vs %f,%f %s", ix, iy, jx, jy, v.Label())
	}
	// panning: grabbed image point follows the mouse
	gx, gy := v.ToImage(500, 500)
	v.PanTo(gx, gy, 520, 470)
	hx, hy := v.ToImage(520, 470)
	if math.Abs(gx-hx) > 1e-9 || math.Abs(gy-hy) > 1e-9 {
		t.Fatal("pan")
	}
	sx, sy := v.ToScreen(hx, hy)
	if math.Abs(sx-520) > 1e-9 || math.Abs(sy-470) > 1e-9 {
		t.Fatal("ToScreen")
	}
	v.ZoomAt(1000, 0, 0)
	if v.Zoom != ZoomMax {
		t.Fatal("max")
	}
}
