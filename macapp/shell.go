//go:build darwin || (linux && gio)

package main

// The window layout of ../main.go: header (icon, title, subtitle, About),
// the module pages with their segmented sub-page bars, and the bottom page
// bar with the four module icons and the version on the right.

import (
	"image"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"basetool/macapp/appstate"
	"basetool/macapp/hdrctl"
	"basetool/macapp/icons"
)

const subtitle = "Camera and light control, tethered HDR scanning, RAW to linear TIFF, crop and Convert (RAW to film negative)"

// UI is the whole window. All fields belong to the window goroutine.
type UI struct {
	th    *Theme
	nav   appstate.Nav
	posts chan func()
	inval func()

	appIcon  paint.ImageOp
	hasIcon  bool
	navBtns  [4]widget.Clickable
	navIcons map[string]paint.ImageOp
	segBtns  [4][]widget.Clickable
	about    Button
	scroll   widget.List

	dialogs []*dialog
	dlgOK   Button
	dlgNo   Button

	hdr  *hdrctl.Ctl
	hp   *hdrPages
	pick func(title string, kind pickKind, start string, exts []string, done func(string, bool))
}

type dialog struct {
	title, text string
	confirm     bool
	done        func(bool)
}

func newUI(th *Theme, inval func()) *UI {
	u := &UI{th: th, posts: make(chan func(), 1024), inval: inval, navIcons: map[string]paint.ImageOp{}}
	if img := loadAppIcon(); img != nil {
		u.appIcon = paint.NewImageOp(img)
		u.hasIcon = true
	}
	for i, m := range appstate.Modules {
		u.segBtns[i] = make([]widget.Clickable, len(m.Subs))
	}
	u.scroll.Axis = layout.Vertical
	u.pick = u.pickPath
	u.hdr = hdrctl.New(u)
	u.hp = newHDRPages(u)
	return u
}

// ---------- hdrctl.Host ----------

// Post queues f for the window goroutine and wakes the window. It never
// blocks the caller and never runs on the macOS main thread.
func (u *UI) Post(f func()) {
	u.posts <- f
	if u.inval != nil {
		u.inval()
	}
}

func (u *UI) drain() {
	for {
		select {
		case f := <-u.posts:
			f()
		default:
			return
		}
	}
}

func (u *UI) PickSimScene(start string, done func(string, bool)) {
	u.pick("Simulated camera: pick an uncompressed RAF to use as the scene", pickOpen, start, []string{"raf", "RAF"}, done)
}

func (u *UI) Message(title, text string) {
	u.dialogs = append(u.dialogs, &dialog{title: title, text: text})
}

func (u *UI) Confirm(title, text string, done func(bool)) {
	u.dialogs = append(u.dialogs, &dialog{title: title, text: text, confirm: true, done: done})
}

// ---------- layout ----------

func (u *UI) setNav() {
	u.hdr.SetLiveVisible(u.nav.LiveView() == 0 || u.nav.LiveView() == 1)
}

func (u *UI) Layout(gtx layout.Context) layout.Dimensions {
	u.drain()
	fillRect(gtx, cBg, image.Rectangle{Max: gtx.Constraints.Max})
	for i := range u.navBtns {
		if u.navBtns[i].Clicked(gtx) && u.nav.SetModule(i) {
			u.setNav()
		}
	}
	for m := range u.segBtns {
		for s := range u.segBtns[m] {
			if u.segBtns[m][s].Clicked(gtx) && m == u.nav.Module && u.nav.SetSub(s) {
				u.setNav()
			}
		}
	}
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.header),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			fillRect(gtx, cBorder, image.Rectangle{Max: sz})
			return layout.Dimensions{Size: sz}
		}),
		layout.Flexed(1, u.module),
		layout.Rigid(u.pageBar),
	)
	u.layoutDialog(gtx)
	return dims
}

func (u *UI) header(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(46)
	w := gtx.Constraints.Max.X
	fillRect(gtx, cBar, image.Rect(0, 0, w, h))
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 14, Right: 14, Top: 5, Bottom: 5}.Layout(g, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				s := gtx.Dp(30)
				if u.hasIcon {
					drawImage(gtx, u.appIcon, image.Rect(0, 0, s, s))
				}
				return layout.Dimensions{Size: image.Pt(s, s)}
			}),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.th.label(gtx, textTitle, rgb(236, 236, 240), appstate.AppVersion, true, false)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(u.th.Theme, textBody, subtitle)
						l.Color, l.MaxLines = rgb(128, 128, 136), 1
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				d, clicked := u.about.Layout(gtx, u.th, "About", btnOpts{minW: 70})
				if clicked {
					u.Message("About", aboutText())
				}
				return d
			}),
		)
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func aboutText() string {
	return appstate.AppVersion + " for macOS (Apple Silicon), Gio user interface (work in progress).\n\n" +
		"Camera and light control, tethered HDR scanning, RAW to linear TIFF, crop and Convert (RAW to film negative).\n" +
		"Ported so far: Camera & Light > Camera and HDR Scan > White light scan. The other pages are placeholders on macOS; use the Windows app for them.\n\n" +
		"Settings: ~/Library/Application Support/NegativeScanStudio (same files as Windows)\nCrash log: ~/Library/Logs/NegativeScanStudio/crash_log.txt"
}

// module is the current module: segment bar (if any) and the page, in a
// scroll view like the Windows ScrollView.
func (u *UI) module(gtx layout.Context) layout.Dimensions {
	m := appstate.Modules[u.nav.Module]
	return layout.Inset{Left: 4, Top: 2, Right: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(m.Subs) == 0 {
					return layout.Dimensions{}
				}
				return u.segment(gtx, u.nav.Module)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return u.page(gtx, u.nav.Page())
			}),
		)
	})
}

// segment is the small segmented selector (navBar with segment = true).
func (u *UI) segment(gtx layout.Context, mod int) layout.Dimensions {
	m := appstate.Modules[mod]
	h := gtx.Dp(34)
	sw, sh := gtx.Dp(150), gtx.Dp(26)
	x0 := gtx.Dp(6)
	y := (h - sh) / 2
	all := image.Rect(x0, y, x0+len(m.Subs)*sw+gtx.Dp(6), y+sh)
	roundBox(gtx, cField, cBorder, all, gtx.Dp(6))
	cur := u.nav.Sub[mod]
	for i, s := range m.Subs {
		r := image.Rect(x0+gtx.Dp(3)+i*sw, y+gtx.Dp(3), x0+gtx.Dp(3)+(i+1)*sw, y+sh-gtx.Dp(3))
		off := op.Offset(r.Min).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(r.Size())
		btn := &u.segBtns[mod][i]
		btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
			txt := cDim
			switch {
			case i == cur:
				roundBox(gtx, rgb(60, 60, 66), rgb(74, 74, 80), image.Rectangle{Max: r.Size()}, gtx.Dp(4))
				txt = cAccent
			case btn.Hovered():
				roundBox(gtx, rgb(40, 40, 44), rgb(40, 40, 44), image.Rectangle{Max: r.Size()}, gtx.Dp(4))
				txt = cText
			}
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				l := material.Label(u.th.Theme, textBody, s)
				l.Color, l.MaxLines = txt, 1
				return l.Layout(gtx)
			})
		})
		off.Pop()
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

func (u *UI) navIcon(name string, col [4]uint8) paint.ImageOp {
	k := name + string(col[:])
	if im, ok := u.navIcons[k]; ok {
		return im
	}
	im := paint.NewImageOp(icons.Render(name, 44, rgb(col[0], col[1], col[2])))
	u.navIcons[k] = im
	return im
}

// pageBar is the bottom page bar: 124-px buttons centred, icon over label,
// orange for the current module, version on the right.
func (u *UI) pageBar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(60)
	w := gtx.Constraints.Max.X
	fillRect(gtx, cBar, image.Rect(0, 0, w, h))
	fillRect(gtx, cBorder, image.Rect(0, 0, w, 1))
	n := len(appstate.Modules)
	bw := gtx.Dp(124)
	x0 := (w - n*bw) / 2
	for i, m := range appstate.Modules {
		r := image.Rect(x0+i*bw, gtx.Dp(5), x0+(i+1)*bw, h-gtx.Dp(4))
		btn := &u.navBtns[i]
		off := op.Offset(r.Min).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Exact(r.Size())
		btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
			col := [4]uint8{150, 150, 158, 255}
			if btn.Hovered() {
				col = [4]uint8{220, 220, 226, 255}
			}
			if i == u.nav.Module {
				col = [4]uint8{cAccent.R, cAccent.G, cAccent.B, 255}
				roundBox(gtx, rgb(36, 36, 40), rgb(36, 36, 40), image.Rectangle{Max: r.Size()}, gtx.Dp(6))
			}
			is := gtx.Dp(22)
			drawImage(gtx, u.navIcon(m.Icon, col), image.Rect((r.Dx()-is)/2, gtx.Dp(5), (r.Dx()-is)/2+is, gtx.Dp(5)+is))
			o2 := op.Offset(image.Pt(0, gtx.Dp(30))).Push(gtx.Ops)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(r.Dx(), gtx.Dp(16)))
			layout.Center.Layout(g, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				l := material.Label(u.th.Theme, textSmall, m.Title)
				l.Color, l.Font.Weight, l.MaxLines = rgb(col[0], col[1], col[2]), font.Bold, 1
				return l.Layout(gtx)
			})
			o2.Pop()
			return layout.Dimensions{Size: r.Size()}
		})
		off.Pop()
		if i == u.nav.Module {
			fillRect(gtx, cAccent, image.Rect(r.Min.X+r.Dx()/2-gtx.Dp(16), 1, r.Min.X+r.Dx()/2+gtx.Dp(16), 1+gtx.Dp(2)))
		}
	}
	// version on the right
	g := gtx
	g.Constraints = layout.Exact(image.Pt(gtx.Dp(206), h))
	off := op.Offset(image.Pt(w-gtx.Dp(220), 0)).Push(gtx.Ops)
	layout.E.Layout(g, func(gtx layout.Context) layout.Dimensions {
		return u.th.label(gtx, textSmall, rgb(100, 100, 108), appstate.AppVersion, false, false)
	})
	off.Pop()
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (u *UI) page(gtx layout.Context, p appstate.Page) layout.Dimensions {
	switch p {
	case appstate.PageCamera:
		return u.hp.camera(gtx)
	case appstate.PageWhite:
		return u.hp.white(gtx)
	}
	return u.placeholder(gtx, p)
}

var pageTitles = map[appstate.Page]string{
	appstate.PageLight: "Light", appstate.PageTri: "Trichrome", appstate.PageMono: "Mono trichrome",
	appstate.PageFocus: "Focus check", appstate.PageCrop: "Crop", appstate.PageMerge: "Merge files",
	appstate.PageHairs: "Remove hairs", appstate.PageBatch: "Batch RAW to TIFF", appstate.PageConvert: "Convert",
}

func (u *UI) placeholder(gtx layout.Context, p appstate.Page) layout.Dimensions {
	return layout.Inset{Left: 12, Top: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.th.label(gtx, unit.Sp(14), cText, pageTitles[p], true, false)
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.th.hint(gtx, "This page is not on macOS yet; it is ported in a later increment. Use the Windows app for it for now.")
			}),
		)
	})
}

// layoutDialog draws the first pending message box over the window.
func (u *UI) layoutDialog(gtx layout.Context) {
	if len(u.dialogs) == 0 {
		return
	}
	d := u.dialogs[0]
	close := func(ok bool) {
		u.dialogs = u.dialogs[1:]
		if d.done != nil {
			d.done(ok)
		}
	}
	// dim and swallow input behind the box
	full := image.Rectangle{Max: gtx.Constraints.Max}
	paint.FillShape(gtx.Ops, colorA(0, 0, 0, 150), clip.Rect(full).Op())
	st := clip.Rect(full).Push(gtx.Ops)
	eventOp(gtx, d)
	st.Pop()
	bw := min(gtx.Dp(520), gtx.Constraints.Max.X-gtx.Dp(40))
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(bw, 0), Max: image.Pt(bw, gtx.Constraints.Max.Y-gtx.Dp(40))}
	var ok, cancel bool
	bd := layout.UniformInset(16).Layout(g, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.th.bold(gtx, d.title, cText) }),
			layout.Rigid(layout.Spacer{Height: 10}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.th.text(gtx, strings.TrimSpace(d.text)) }),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Spacing: layout.SpaceStart}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !d.confirm {
							return layout.Dimensions{}
						}
						dd, c := u.dlgNo.Layout(gtx, u.th, "Cancel", btnOpts{minW: 80})
						cancel = c
						return dd
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						dd, c := u.dlgOK.Layout(gtx, u.th, "OK", btnOpts{minW: 80, primary: true})
						ok = c
						return dd
					}),
				)
			}),
		)
	})
	call := m.Stop()
	r := image.Rectangle{Max: bd.Size}.Add(image.Pt((gtx.Constraints.Max.X-bd.Size.X)/2, (gtx.Constraints.Max.Y-bd.Size.Y)/3))
	roundBox(gtx, cPanel, cAccent, r, gtx.Dp(6))
	off := op.Offset(r.Min).Push(gtx.Ops)
	call.Add(gtx.Ops)
	off.Pop()
	if ok {
		close(true)
	} else if cancel {
		close(false)
	}
}
