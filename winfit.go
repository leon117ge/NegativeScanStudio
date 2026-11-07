package main

import (
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// fitToWorkArea keeps the main window inside the work area of its monitor
// (screen minus taskbar) so the title bar with minimize / maximize / close is
// always reachable, whatever the screen size or display scaling. The window
// is shrunk if needed (the pages scroll), centered, and shown normally, not
// maximized.
func fitToWorkArea(mw *walk.MainWindow) {
	hwnd := mw.Handle()
	var mi win.MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if !win.GetMonitorInfo(win.MonitorFromWindow(hwnd, win.MONITOR_DEFAULTTONEAREST), &mi) {
		return
	}
	wa := mi.RcWork
	waW, waH := int(wa.Right-wa.Left), int(wa.Bottom-wa.Top)
	if waW < 200 || waH < 200 {
		return
	}
	if win.IsZoomed(hwnd) {
		win.ShowWindow(hwnd, win.SW_RESTORE)
	}
	b := mw.BoundsPixels()
	w, h := b.Width, b.Height
	if w > waW {
		w = waW
	}
	if h > waH {
		h = waH
	}
	x, y := b.X, b.Y
	if x < int(wa.Left) || x+w > int(wa.Right) || y < int(wa.Top) || y+h > int(wa.Bottom) || w != b.Width || h != b.Height {
		x = int(wa.Left) + (waW-w)/2
		y = int(wa.Top) + (waH-h)/2
	}
	mw.SetBoundsPixels(walk.Rectangle{X: x, Y: y, Width: w, Height: h})
}

// setDropWidth widens a combo box's drop-down list (CB_SETDROPPEDWIDTH) to
// at least w pixels at 96 DPI without changing the closed control.
func setDropWidth(cb *walk.ComboBox, w int) {
	if cb == nil {
		return
	}
	win.SendMessage(cb.Handle(), win.CB_SETDROPPEDWIDTH, uintptr(cb.IntFrom96DPI(w)), 0)
}
