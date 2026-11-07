package main

// Dark, DaVinci Resolve-like theme. Everything here is applied after the
// declarative UI has been created: colours on walk widgets, custom drawing of
// push buttons / check boxes (NM_CUSTOMDRAW), combo boxes, group boxes and
// edit borders through window subclassing, dark progress bars and table views.
// All extra system DLL entry points are loaded lazily so the static import
// table of the exe does not change.

import (
	"strings"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

var (
	cBg         = walk.RGB(31, 31, 34)    // window / page background
	cPanel      = walk.RGB(42, 42, 46)    // section panels
	cBorder     = walk.RGB(58, 58, 62)    // 1 px panel borders
	cHeaderText = walk.RGB(154, 154, 160) // section headers
	cText       = walk.RGB(216, 216, 220)
	cDim        = walk.RGB(138, 138, 146)
	cField      = walk.RGB(24, 24, 26) // inputs
	cFieldLine  = walk.RGB(66, 66, 72)
	cAccent     = walk.RGB(232, 124, 44) // the app's orange
	cAccentHot  = walk.RGB(246, 146, 66)
	cAccentDown = walk.RGB(196, 100, 32)
	cBtn        = walk.RGB(58, 58, 63)
	cBtnHot     = walk.RGB(72, 72, 78)
	cBtnDown    = walk.RGB(44, 44, 48)
	cBtnLine    = walk.RGB(78, 78, 84)
	cBtnOff     = walk.RGB(46, 46, 50)
	cTextOff    = walk.RGB(104, 104, 110)
	cBar        = walk.RGB(24, 24, 27) // bottom page bar / header
	cRed        = walk.RGB(240, 96, 86)
	cGreen      = walk.RGB(96, 204, 116)
	cBlue       = walk.RGB(100, 150, 245)
	cOK         = walk.RGB(96, 204, 128)
	cWarn       = walk.RGB(240, 100, 86)
	cListSel    = walk.RGB(70, 58, 48)
)

// mapColor turns the light-theme colours used in the page code into their
// dark-theme counterparts.
func mapColor(c walk.Color) walk.Color {
	switch c {
	case 0:
		return cText
	case walk.RGB(110, 110, 110), walk.RGB(130, 130, 130):
		return cDim
	case walk.RGB(30, 110, 60):
		return cOK
	case walk.RGB(200, 50, 50):
		return cRed
	case walk.RGB(40, 150, 60):
		return cGreen
	case walk.RGB(50, 90, 200):
		return cBlue
	}
	return c
}

func init() {
	walk.DarkTableColors = &walk.TableViewDarkColors{
		BG: walk.RGB(26, 26, 29), Text: cText,
		SelBG: cListSel, SelText: walk.RGB(255, 255, 255), SelNoFocusBG: walk.RGB(56, 52, 50),
		AltBG: walk.RGB(32, 32, 36), AltText: cText,
		HeaderBG: walk.RGB(40, 40, 44), HeaderText: cHeaderText, HeaderLine: cBorder,
	}
	allowDarkApp()
}

// ---------- lazily loaded system calls ----------

var (
	lzUser32    = syscall.NewLazyDLL("user32.dll")
	lzGdi32     = syscall.NewLazyDLL("gdi32.dll")
	pGetWDC     = lzUser32.NewProc("GetWindowDC")
	pGetWText   = lzUser32.NewProc("GetWindowTextW")
	pGetWTextL  = lzUser32.NewProc("GetWindowTextLengthW")
	pTrackMouse = lzUser32.NewProc("TrackMouseEvent")
	pSolidBr    = lzGdi32.NewProc("CreateSolidBrush")
)

func callOK(p *syscall.LazyProc) bool { return p.Find() == nil }

var brushCache = map[walk.Color]uintptr{}

func solidBrush(c walk.Color) uintptr {
	if b, ok := brushCache[c]; ok {
		return b
	}
	b, _, _ := pSolidBr.Call(uintptr(c))
	brushCache[c] = b
	return b
}

func windowText(h win.HWND) string {
	n, _, _ := pGetWTextL.Call(uintptr(h))
	buf := make([]uint16, n+2)
	pGetWText.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// allowDarkApp opts the process into dark scroll bars / menus on Windows 10
// 1809+ (uxtheme ordinal 135); ignored where it does not exist.
func allowDarkApp() {
	if !winBuildAtLeast(17763) {
		return
	}
	if p := uxOrdinal(135); p != 0 {
		syscall.SyscallN(p, 1)
	}
}

func allowDarkWindow(h win.HWND) {
	if !winBuildAtLeast(17763) {
		return
	}
	if p := uxOrdinal(133); p != 0 {
		syscall.SyscallN(p, uintptr(h), 1)
	}
}

var uxHandle syscall.Handle

func uxOrdinal(n uintptr) uintptr {
	if uxHandle == 0 {
		h, err := syscall.LoadLibrary("uxtheme.dll")
		if err != nil {
			return 0
		}
		uxHandle = h
	}
	gpa := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcAddress")
	r, _, _ := gpa.Call(uintptr(uxHandle), n)
	return r
}

func winBuildAtLeast(b uint32) bool {
	type osvi struct {
		size, major, minor, build, platform uint32
		csd                                 [128]uint16
	}
	p := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if p.Find() != nil {
		return false
	}
	var v osvi
	v.size = uint32(unsafe.Sizeof(v))
	p.Call(uintptr(unsafe.Pointer(&v)))
	return v.major > 10 || (v.major == 10 && v.build >= b)
}

// darkTitleBar asks DWM for a dark caption (attribute 20, 19 on older builds).
func darkTitleBar(h win.HWND) {
	p := syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	if p.Find() != nil {
		return
	}
	on := int32(1)
	if r, _, _ := p.Call(uintptr(h), 20, uintptr(unsafe.Pointer(&on)), 4); r != 0 {
		p.Call(uintptr(h), 19, uintptr(unsafe.Pointer(&on)), 4)
	}
}

func setTheme(h win.HWND, app string) {
	var a *uint16
	if app != "-" {
		a = syscall.StringToUTF16Ptr(app)
	}
	e := syscall.StringToUTF16Ptr("")
	if app == "-" {
		win.SetWindowTheme(h, e, e)
		return
	}
	win.SetWindowTheme(h, a, nil)
}

// ---------- subclassing ----------

const (
	roleParent = 1 << iota
	roleCombo
	roleEdit
	roleGroup
	roleButton
	roleHover
	roleProgress
)

type subInfo struct {
	roles int
	orig  uintptr
	w     walk.Window // owning walk widget (for DPI / font / text)
	hot   bool
}

var (
	subs      = map[win.HWND]*subInfo{}
	drawn     = map[win.HWND]walk.Widget{} // push buttons and check boxes drawn by us
	bgOfHwnd  = map[win.HWND]walk.Color{}
	subProcCB = syscall.NewCallback(subProc)
)

func subclass(h win.HWND, role int, w walk.Window) {
	if h == 0 {
		return
	}
	if si, ok := subs[h]; ok {
		si.roles |= role
		if w != nil && si.w == nil {
			si.w = w
		}
		return
	}
	si := &subInfo{roles: role, w: w}
	subs[h] = si
	si.orig = win.SetWindowLongPtr(h, win.GWLP_WNDPROC, subProcCB)
}

// bgAt returns the background colour behind window h.
func bgAt(h win.HWND) walk.Color {
	for x := h; x != 0; x = win.GetParent(x) {
		if c, ok := bgOfHwnd[x]; ok {
			return c
		}
	}
	return cBg
}

func dpiOf(w walk.Window) int {
	if w != nil {
		if d := w.DPI(); d > 0 {
			return d
		}
	}
	return 96
}

func px(v int, dpi int) int { return walk.IntFrom96DPI(v, dpi) }

const (
	wmCtlColorEdit    = 0x0133
	wmCtlColorListbox = 0x0134
	wmCtlColorBtn     = 0x0135
	wmCtlColorStatic  = 0x0138
	wmNCPaint         = 0x0085
	wmMouseLeave      = 0x02A3
	cbSetCurSel       = 0x014E
	cbShowDropdown    = 0x014F
	wmSetFocus        = 0x0007
	wmKillFocus       = 0x0008
	wmEnable          = 0x000A
)

func subProc(h win.HWND, msg uint32, wp, lp uintptr) uintptr {
	si := subs[h]
	if si == nil {
		return win.DefWindowProc(h, msg, wp, lp)
	}
	if si.roles&roleParent != 0 {
		switch msg {
		case win.WM_NOTIFY:
			nmh := (*win.NMHDR)(unsafe.Pointer(lp))
			if w, ok := drawn[nmh.HwndFrom]; ok && nmh.Code == win.NM_CUSTOMDRAW {
				return drawButtonCD(w, (*win.NMCUSTOMDRAW)(unsafe.Pointer(lp)))
			}
		case wmCtlColorStatic, wmCtlColorBtn:
			if _, ok := drawn[win.HWND(lp)]; ok {
				bg := bgAt(win.HWND(lp))
				win.SetTextColor(win.HDC(wp), win.COLORREF(cText))
				win.SetBkColor(win.HDC(wp), win.COLORREF(bg))
				return solidBrush(bg)
			}
		case wmCtlColorListbox:
			win.SetTextColor(win.HDC(wp), win.COLORREF(cText))
			win.SetBkColor(win.HDC(wp), win.COLORREF(cField))
			return solidBrush(cField)
		case wmCtlColorEdit:
			if s, ok := subs[win.HWND(lp)]; ok && s.roles&roleCombo != 0 {
				win.SetTextColor(win.HDC(wp), win.COLORREF(cText))
				win.SetBkColor(win.HDC(wp), win.COLORREF(comboFill))
				return solidBrush(comboFill)
			}
		}
	}
	if si.roles&roleCombo != 0 {
		switch msg {
		case win.WM_PAINT:
			paintCombo(h, si)
			return 0
		case win.WM_ERASEBKGND:
			return 1
		case wmCtlColorListbox:
			win.SetTextColor(win.HDC(wp), win.COLORREF(cText))
			win.SetBkColor(win.HDC(wp), win.COLORREF(cField))
			return solidBrush(cField)
		case wmSetFocus, wmKillFocus, wmEnable, cbSetCurSel, cbShowDropdown, win.WM_LBUTTONUP, win.WM_KEYDOWN, win.WM_CAPTURECHANGED:
			r := win.CallWindowProc(si.orig, h, msg, wp, lp)
			win.InvalidateRect(h, nil, false)
			return r
		case win.WM_MOUSEMOVE:
			trackHover(h, si)
		case wmMouseLeave:
			si.hot = false
			win.InvalidateRect(h, nil, false)
		}
	}
	if si.roles&roleHover != 0 && msg == wmMouseLeave {
		if f := hoverLeave[h]; f != nil {
			f()
		}
	}
	if si.roles&roleEdit != 0 && msg == wmNCPaint {
		r := win.CallWindowProc(si.orig, h, msg, wp, lp)
		paintEditBorder(h)
		return r
	}
	if si.roles&roleProgress != 0 {
		switch msg {
		case win.WM_PAINT:
			paintProgress(h, si)
			return 0
		case win.WM_ERASEBKGND:
			return 1
		case 0x0402, 0x0405, 0x0406, 0x0401, 0x0403: // PBM_SETPOS, SETSTEP, STEPIT, SETRANGE, DELTAPOS
			r := win.CallWindowProc(si.orig, h, msg, wp, lp)
			win.InvalidateRect(h, nil, false)
			return r
		}
	}
	if si.roles&roleGroup != 0 {
		switch msg {
		case win.WM_PAINT:
			paintGroup(h, si)
			return 0
		case win.WM_ERASEBKGND:
			return 1
		}
	}
	return win.CallWindowProc(si.orig, h, msg, wp, lp)
}

func trackHover(h win.HWND, si *subInfo) {
	if si.hot {
		return
	}
	si.hot = true
	win.InvalidateRect(h, nil, false)
	tme := win.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), DwFlags: win.TME_LEAVE, HwndTrack: h}
	pTrackMouse.Call(uintptr(unsafe.Pointer(&tme)))
}

var hoverLeave = map[win.HWND]func(){}

func trackLeave(h win.HWND) {
	tme := win.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), DwFlags: win.TME_LEAVE, HwndTrack: h}
	pTrackMouse.Call(uintptr(unsafe.Pointer(&tme)))
}

// ---------- drawing ----------

func fill(c *walk.Canvas, col walk.Color, r walk.Rectangle) {
	if b, err := walk.NewSolidColorBrush(col); err == nil {
		c.FillRectanglePixels(b, r)
		b.Dispose()
	}
}

func roundBox(c *walk.Canvas, fillCol, line walk.Color, r walk.Rectangle, rad int) {
	b, _ := walk.NewSolidColorBrush(fillCol)
	p, _ := walk.NewCosmeticPen(walk.PenSolid, line)
	if b != nil {
		c.FillRoundedRectanglePixels(b, r, walk.Size{Width: rad, Height: rad})
		b.Dispose()
	}
	if p != nil {
		c.DrawRoundedRectanglePixels(p, r, walk.Size{Width: rad, Height: rad})
		p.Dispose()
	}
}

func line(c *walk.Canvas, col walk.Color, width int, pts ...walk.Point) {
	var pen walk.Pen
	var err error
	if width <= 1 {
		pen, err = walk.NewCosmeticPen(walk.PenSolid, col)
	} else {
		b, e := walk.NewSolidColorBrush(col)
		if e != nil {
			return
		}
		defer b.Dispose()
		pen, err = walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, width, b)
	}
	if err != nil {
		return
	}
	defer pen.Dispose()
	c.DrawPolylinePixels(pen, pts)
}

const (
	cdisSelected = 0x0001
	cdisDisabled = 0x0004
	cdisFocus    = 0x0010
	cdisHot      = 0x0040
)

func drawButtonCD(w walk.Widget, nm *win.NMCUSTOMDRAW) uintptr {
	if nm.DwDrawStage != win.CDDS_PREPAINT {
		return win.CDRF_DODEFAULT
	}
	c, err := walk.NewCanvasFromHDC(nm.Hdc)
	if err != nil {
		return win.CDRF_DODEFAULT
	}
	defer c.Dispose()
	dpi := dpiOf(w)
	r := walk.Rectangle{X: int(nm.Rc.Left), Y: int(nm.Rc.Top), Width: int(nm.Rc.Right - nm.Rc.Left), Height: int(nm.Rc.Bottom - nm.Rc.Top)}
	st := nm.UItemState
	enabled := w.Enabled() && st&cdisDisabled == 0
	fill(c, bgAt(w.Handle()), r)
	font := w.Font()
	switch b := w.(type) {
	case *walk.PushButton:
		primary := font != nil && font.Bold()
		face, edge, txt := cBtn, cBtnLine, cText
		switch {
		case !enabled:
			face, edge, txt = cBtnOff, walk.RGB(54, 54, 58), cTextOff
		case primary && st&cdisSelected != 0:
			face, edge, txt = cAccentDown, cAccentDown, walk.RGB(255, 255, 255)
		case primary && st&cdisHot != 0:
			face, edge, txt = cAccentHot, cAccentHot, walk.RGB(255, 255, 255)
		case primary:
			face, edge, txt = cAccent, cAccent, walk.RGB(255, 255, 255)
		case st&cdisSelected != 0:
			face, edge = cBtnDown, cBtnLine
		case st&cdisHot != 0:
			face = cBtnHot
		}
		if !enabled && primary {
			face, edge, txt = walk.RGB(70, 56, 46), walk.RGB(80, 62, 50), walk.RGB(140, 120, 104)
		}
		if enabled && st&cdisFocus != 0 && !primary {
			edge = cAccent
		}
		in := walk.Rectangle{X: r.X + 1, Y: r.Y + 1, Width: r.Width - 2, Height: r.Height - 2}
		roundBox(c, face, edge, in, px(5, dpi))
		tr := in
		if st&cdisSelected != 0 {
			tr.Y++
		}
		c.DrawTextPixels(strings.ReplaceAll(b.Text(), "&", "&&"), font, txt, tr, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
	case *walk.CheckBox:
		box := px(13, dpi)
		br := walk.Rectangle{X: r.X, Y: r.Y + (r.Height-box)/2, Width: box, Height: box}
		checked := b.Checked()
		face, edge := cField, walk.RGB(96, 96, 104)
		if st&cdisHot != 0 || st&cdisFocus != 0 {
			edge = cAccent
		}
		if checked {
			face, edge = cAccent, cAccent
			if st&cdisHot != 0 {
				face, edge = cAccentHot, cAccentHot
			}
		}
		if !enabled {
			face, edge = cBtnOff, walk.RGB(70, 70, 76)
		}
		roundBox(c, face, edge, br, px(3, dpi))
		if checked {
			s := float64(box) / 13
			p := func(x, y float64) walk.Point { return walk.Point{X: br.X + int(x*s+0.5), Y: br.Y + int(y*s+0.5)} }
			col := walk.RGB(255, 255, 255)
			if !enabled {
				col = cTextOff
			}
			line(c, col, px(2, dpi), p(3.2, 7.2), p(6, 10), p(10.8, 4.2))
		}
		txt := cText
		if !enabled {
			txt = cTextOff
		}
		tr := walk.Rectangle{X: br.X + box + px(4, dpi), Y: r.Y, Width: r.Width - box - px(4, dpi), Height: r.Height}
		c.DrawTextPixels(strings.ReplaceAll(b.Text(), "&", "&&"), font, txt, tr, walk.TextLeft|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
	default:
		return win.CDRF_DODEFAULT
	}
	return win.CDRF_SKIPDEFAULT
}

var comboFill = walk.RGB(48, 48, 53)

func paintCombo(h win.HWND, si *subInfo) {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(h, &ps)
	defer win.EndPaint(h, &ps)
	c, err := walk.NewCanvasFromHDC(hdc)
	if err != nil {
		return
	}
	defer c.Dispose()
	var cr win.RECT
	win.GetClientRect(h, &cr)
	dpi := dpiOf(si.w)
	r := walk.Rectangle{Width: int(cr.Right), Height: int(cr.Bottom)}
	fill(c, bgAt(h), r)
	enabled := win.IsWindowEnabled(h)
	face, edge, txt := comboFill, cFieldLine, cText
	if si.hot {
		face = walk.RGB(58, 58, 64)
	}
	if win.GetFocus() == h {
		edge = cAccent
	}
	if !enabled {
		face, edge, txt = cBtnOff, walk.RGB(54, 54, 58), cTextOff
	}
	roundBox(c, face, edge, walk.Rectangle{X: 0, Y: 0, Width: r.Width, Height: r.Height}, px(4, dpi))
	aw := px(20, dpi)
	pad := px(7, dpi)
	tr := walk.Rectangle{X: pad, Y: 0, Width: r.Width - aw - pad, Height: r.Height}
	if si.w != nil {
		c.DrawTextPixels(windowText(h), si.w.Font(), txt, tr, walk.TextLeft|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
	}
	// chevron
	cx, cy := r.Width-aw/2-px(2, dpi), r.Height/2
	d := px(4, dpi)
	ac := cHeaderText
	if !enabled {
		ac = cTextOff
	}
	line(c, ac, px(2, dpi)-0, walk.Point{X: cx - d, Y: cy - d/2}, walk.Point{X: cx, Y: cy + d/2}, walk.Point{X: cx + d, Y: cy - d/2})
}

func paintEditBorder(h win.HWND) {
	hdcU, _, _ := pGetWDC.Call(uintptr(h))
	if hdcU == 0 {
		return
	}
	hdc := win.HDC(hdcU)
	defer win.ReleaseDC(h, hdc)
	var wr win.RECT
	win.GetWindowRect(h, &wr)
	w, ht := int(wr.Right-wr.Left), int(wr.Bottom-wr.Top)
	c, err := walk.NewCanvasFromHDC(hdc)
	if err != nil {
		return
	}
	defer c.Dispose()
	outer, inner := cFieldLine, cField
	if win.GetFocus() == h {
		outer = walk.RGB(120, 90, 64)
	}
	if p, e := walk.NewCosmeticPen(walk.PenSolid, outer); e == nil {
		c.DrawRectanglePixels(p, walk.Rectangle{Width: w, Height: ht})
		p.Dispose()
	}
	if p, e := walk.NewCosmeticPen(walk.PenSolid, inner); e == nil {
		c.DrawRectanglePixels(p, walk.Rectangle{X: 1, Y: 1, Width: w - 2, Height: ht - 2})
		p.Dispose()
	}
}

var sectionFont *walk.Font

func paintGroup(h win.HWND, si *subInfo) {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(h, &ps)
	defer win.EndPaint(h, &ps)
	var compTop int32
	// the content composite is a sibling on top of us: never paint over it
	for x := win.GetWindow(win.GetParent(h), win.GW_CHILD); x != 0; x = win.GetWindow(x, win.GW_HWNDNEXT) {
		if x == h || !win.IsWindowVisible(x) {
			continue
		}
		var wr win.RECT
		win.GetWindowRect(x, &wr)
		a := win.POINT{X: wr.Left, Y: wr.Top}
		b := win.POINT{X: wr.Right, Y: wr.Bottom}
		win.ScreenToClient(h, &a)
		win.ScreenToClient(h, &b)
		win.ExcludeClipRect(hdc, a.X, a.Y, b.X, b.Y)
		if a.Y > 0 && (compTop == 0 || a.Y < compTop) {
			compTop = a.Y
		}
	}
	c, err := walk.NewCanvasFromHDC(hdc)
	if err != nil {
		return
	}
	defer c.Dispose()
	var cr win.RECT
	win.GetClientRect(h, &cr)
	dpi := dpiOf(si.w)
	r := walk.Rectangle{Width: int(cr.Right), Height: int(cr.Bottom)}
	fill(c, bgAt(win.GetParent(win.GetParent(h))), r)
	roundBox(c, cPanel, cBorder, r, px(6, dpi))
	if sectionFont == nil {
		sectionFont, _ = walk.NewFont("Segoe UI", 8, walk.FontBold)
	}
	title := strings.ToUpper(windowText(h))
	if title != "" && sectionFont != nil {
		th := px(18, dpi)
		if compTop > 0 {
			th = int(compTop) + px(1, dpi)
		}
		tr := walk.Rectangle{X: px(10, dpi), Y: px(1, dpi), Width: r.Width - px(20, dpi), Height: th}
		c.DrawTextPixels(title, sectionFont, cHeaderText, tr, walk.TextLeft|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
	}
}

// ---------- applying ----------

var (
	fieldBrush, _ = walk.NewSolidColorBrush(cField)
	panelBrush, _ = walk.NewSolidColorBrush(cPanel)
	bgBrush, _    = walk.NewSolidColorBrush(cBg)
)

// applyDark themes w and everything below it. It is safe to call more than
// once.
func applyDark(w walk.Window) {
	if sb, ok := w.Background().(*walk.SolidColorBrush); ok && sb != nil {
		bgOfHwnd[w.Handle()] = sb.Color()
	}
	switch x := w.(type) {
	case *walk.MainWindow:
		x.SetBackground(bgBrush)
		bgOfHwnd[x.Handle()] = cBg
		darkTitleBar(x.Handle())
	case *walk.GroupBox:
		x.SetBackground(panelBrush)
		bgOfHwnd[x.Handle()] = cPanel
		subclass(x.NativeGroupBoxHandle(), roleGroup, x)
		win.InvalidateRect(x.NativeGroupBoxHandle(), nil, true)
	case *walk.Label:
		x.SetTextColor(mapColor(x.TextColor()))
	case *walk.LineEdit:
		x.SetTextColor(mapColor(x.TextColor()))
		x.SetBackground(fieldBrush)
		subclass(x.Handle(), roleEdit, x)
		redrawFrame(x.Handle())
	case *walk.TextEdit:
		x.SetTextColor(mapColor(x.TextColor()))
		x.SetBackground(fieldBrush)
		allowDarkWindow(x.Handle())
		setTheme(x.Handle(), "DarkMode_Explorer")
		subclass(x.Handle(), roleEdit, x)
		redrawFrame(x.Handle())
	case *walk.PushButton:
		drawn[x.Handle()] = x
		subclass(win.GetParent(x.Handle()), roleParent, nil)
	case *walk.CheckBox:
		drawn[x.Handle()] = x
		subclass(win.GetParent(x.Handle()), roleParent, nil)
	case *walk.ComboBox:
		subclass(x.Handle(), roleCombo, x)
		subclass(win.GetParent(x.Handle()), roleParent, nil)
		allowDarkWindow(x.Handle())
		setTheme(x.Handle(), "DarkMode_CFD")
	case *walk.ProgressBar:
		subclass(x.Handle(), roleProgress, x)
		win.InvalidateRect(x.Handle(), nil, true)
	case *walk.TableView:
		allowDarkWindow(x.Handle())
		x.SetDarkListTheme()
	case *walk.ScrollView:
		allowDarkWindow(x.Handle())
		setTheme(x.Handle(), "DarkMode_Explorer")
	}
	if ct, ok := w.(walk.Container); ok && ct.Children() != nil {
		for i := 0; i < ct.Children().Len(); i++ {
			applyDark(ct.Children().At(i))
		}
	}
}

func redrawFrame(h win.HWND) {
	win.SetWindowPos(h, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED|win.SWP_NOACTIVATE)
}

func paintProgress(h win.HWND, si *subInfo) {
	var ps win.PAINTSTRUCT
	hdc := win.BeginPaint(h, &ps)
	defer win.EndPaint(h, &ps)
	c, err := walk.NewCanvasFromHDC(hdc)
	if err != nil {
		return
	}
	defer c.Dispose()
	var cr win.RECT
	win.GetClientRect(h, &cr)
	dpi := dpiOf(si.w)
	r := walk.Rectangle{Width: int(cr.Right), Height: int(cr.Bottom)}
	fill(c, bgAt(win.GetParent(h)), r)
	rad := px(4, dpi)
	roundBox(c, cField, cFieldLine, r, rad)
	lo := int(win.SendMessage(h, 0x0407, 1, 0)) // PBM_GETRANGE low
	hi := int(win.SendMessage(h, 0x0407, 0, 0))
	pos := int(win.SendMessage(h, 0x0408, 0, 0)) // PBM_GETPOS
	if hi > lo && pos > lo {
		w := (r.Width - 2) * (pos - lo) / (hi - lo)
		if w > 0 {
			roundBox(c, cAccent, cAccent, walk.Rectangle{X: 1, Y: 1, Width: w, Height: r.Height - 2}, rad)
		}
	}
}
