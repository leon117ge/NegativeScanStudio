// Copyright 2026 local patch for Film Base Meter: dark colours for TableView.

//go:build windows
// +build windows

package walk

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

// TableViewDarkColors overrides the system / theme colours of every TableView
// when DarkTableColors is set before the window is created.
type TableViewDarkColors struct {
	BG, Text, SelBG, SelText, SelNoFocusBG, AltBG, AltText Color
	HeaderBG, HeaderText, HeaderLine                       Color
}

// DarkTableColors, when non-nil, is applied to all TableViews.
var DarkTableColors *TableViewDarkColors

func (tv *TableView) applyDarkColors() {
	c := DarkTableColors
	tv.themeNormalBGColor = c.BG
	tv.themeNormalTextColor = c.Text
	tv.themeSelectedBGColor = c.SelBG
	tv.themeSelectedTextColor = c.SelText
	tv.themeSelectedNotFocusedBGColor = c.SelNoFocusBG
	tv.alternatingRowBGColor = c.AltBG
	tv.alternatingRowTextColor = c.AltText
	for _, h := range []win.HWND{tv.hwndNormalLV, tv.hwndFrozenLV} {
		if h == 0 {
			continue
		}
		win.SendMessage(h, win.LVM_SETBKCOLOR, 0, uintptr(c.BG))
		win.SendMessage(h, win.LVM_SETTEXTCOLOR, 0, uintptr(c.Text))
		win.SendMessage(h, win.LVM_SETTEXTBKCOLOR, 0, uintptr(c.BG))
	}
}

// SetDarkListTheme gives the list views the Windows 10/11 dark visual style
// (dark scroll bars and selection); it is a no-op where that theme is missing.
func (tv *TableView) SetDarkListTheme() {
	for _, h := range []win.HWND{tv.hwndNormalLV, tv.hwndFrozenLV} {
		if h != 0 {
			win.SetWindowTheme(h, syscall.StringToUTF16Ptr("DarkMode_Explorer"), nil)
		}
	}
	tv.ApplySysColors()
	tv.Invalidate()
}

func darkHeaderCustomDraw(tv *TableView, hwnd win.HWND, lp uintptr) uintptr {
	c := DarkTableColors
	nmcd := (*win.NMCUSTOMDRAW)(unsafe.Pointer(lp))
	switch nmcd.DwDrawStage {
	case win.CDDS_PREPAINT:
		return win.CDRF_NOTIFYITEMDRAW | win.CDRF_NOTIFYPOSTPAINT

	case win.CDDS_ITEMPREPAINT:
		canvas, err := newCanvasFromHDC(nmcd.Hdc)
		if err != nil {
			return win.CDRF_DODEFAULT
		}
		defer canvas.Dispose()
		rc := rectangleFromRECT(nmcd.Rc)
		if bg, err := NewSolidColorBrush(c.HeaderBG); err == nil {
			canvas.FillRectanglePixels(bg, rc)
			bg.Dispose()
		}
		if pen, err := NewCosmeticPen(PenSolid, c.HeaderLine); err == nil {
			canvas.DrawLinePixels(pen, Point{rc.X + rc.Width - 1, rc.Y + 3}, Point{rc.X + rc.Width - 1, rc.Y + rc.Height - 3})
			canvas.DrawLinePixels(pen, Point{rc.X, rc.Y + rc.Height - 1}, Point{rc.X + rc.Width, rc.Y + rc.Height - 1})
			pen.Dispose()
		}
		var buf [256]uint16
		hdi := win.HDITEM{Mask: win.HDI_TEXT | win.HDI_FORMAT, PszText: &buf[0], CchTextMax: int32(len(buf))}
		win.SendMessage(hwnd, win.HDM_GETITEM, nmcd.DwItemSpec, uintptr(unsafe.Pointer(&hdi)))
		text := syscall.UTF16ToString(buf[:])
		pad := IntFrom96DPI(6, tv.DPI())
		tr := Rectangle{rc.X + pad, rc.Y, rc.Width - 2*pad, rc.Height}
		format := TextVCenter | TextSingleLine | TextEndEllipsis
		switch {
		case hdi.Fmt&win.HDF_RIGHT != 0:
			format |= TextRight
		case hdi.Fmt&win.HDF_CENTER != 0:
			format |= TextCenter
		default:
			format |= TextLeft
		}
		canvas.DrawTextPixels(text, tv.Font(), c.HeaderText, tr, format)
		return win.CDRF_SKIPDEFAULT

	case win.CDDS_POSTPAINT:
		var cr win.RECT
		win.GetClientRect(hwnd, &cr)
		left := int32(0)
		if n := int(win.SendMessage(hwnd, win.HDM_GETITEMCOUNT, 0, 0)); n > 0 {
			for i := 0; i < n; i++ {
				var r win.RECT
				win.SendMessage(hwnd, win.HDM_GETITEMRECT, uintptr(i), uintptr(unsafe.Pointer(&r)))
				if r.Right > left {
					left = r.Right
				}
			}
		}
		if left < cr.Right {
			canvas, err := newCanvasFromHDC(nmcd.Hdc)
			if err == nil {
				if bg, err := NewSolidColorBrush(c.HeaderBG); err == nil {
					canvas.FillRectanglePixels(bg, Rectangle{int(left), 0, int(cr.Right - left), int(cr.Bottom)})
					bg.Dispose()
				}
				if pen, err := NewCosmeticPen(PenSolid, c.HeaderLine); err == nil {
					canvas.DrawLinePixels(pen, Point{int(left), int(cr.Bottom) - 1}, Point{int(cr.Right), int(cr.Bottom) - 1})
					pen.Dispose()
				}
				canvas.Dispose()
			}
		}
		return win.CDRF_DODEFAULT
	}
	return win.CDRF_DODEFAULT
}

// NewCanvasFromHDC wraps a device context owned by someone else (the canvas
// never releases it).
func NewCanvasFromHDC(hdc win.HDC) (*Canvas, error) { return newCanvasFromHDC(hdc) }

// NativeGroupBoxHandle returns the BUTTON (BS_GROUPBOX) window that draws the
// frame and title of the group box.
func (gb *GroupBox) NativeGroupBoxHandle() win.HWND { return gb.hWndGroupBox }

// darkEraseLV paints the empty part of a list view (Wine does not do it when
// the erase is skipped for double buffering).
func darkEraseLV(hwnd win.HWND, hdc win.HDC) {
	var rc win.RECT
	win.GetClientRect(hwnd, &rc)
	c, err := newCanvasFromHDC(hdc)
	if err != nil {
		return
	}
	defer c.Dispose()
	if b, err := NewSolidColorBrush(DarkTableColors.BG); err == nil {
		c.FillRectanglePixels(b, rectangleFromRECT(rc))
		b.Dispose()
	}
}
