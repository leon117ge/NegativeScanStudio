package main

import (
	"sync"

	"github.com/lxn/walk"
)

type penKey struct {
	col   walk.Color
	width int
}

type fontKey struct {
	family string
	pt     int
	style  walk.FontStyle
}

var paintCache struct {
	mu      sync.Mutex
	brushes map[walk.Color]*walk.SolidColorBrush
	pens    map[penKey]*walk.GeometricPen
	fonts   map[fontKey]*walk.Font
}

func cachedBrush(col walk.Color) *walk.SolidColorBrush {
	paintCache.mu.Lock()
	defer paintCache.mu.Unlock()
	if paintCache.brushes == nil {
		paintCache.brushes = make(map[walk.Color]*walk.SolidColorBrush)
	}
	if b, ok := paintCache.brushes[col]; ok {
		return b
	}
	b, err := walk.NewSolidColorBrush(col)
	if err != nil || b == nil {
		return nil
	}
	paintCache.brushes[col] = b
	return b
}

func cachedPen(col walk.Color, width int) *walk.GeometricPen {
	paintCache.mu.Lock()
	defer paintCache.mu.Unlock()
	if paintCache.pens == nil {
		paintCache.pens = make(map[penKey]*walk.GeometricPen)
	}
	k := penKey{col: col, width: width}
	if p, ok := paintCache.pens[k]; ok {
		return p
	}
	b := paintCache.brushes[col]
	if b == nil {
		var err error
		b, err = walk.NewSolidColorBrush(col)
		if err != nil || b == nil {
			return nil
		}
		if paintCache.brushes == nil {
			paintCache.brushes = make(map[walk.Color]*walk.SolidColorBrush)
		}
		paintCache.brushes[col] = b
	}
	p, err := walk.NewGeometricPen(walk.PenSolid, width, b)
	if err != nil || p == nil {
		return nil
	}
	paintCache.pens[k] = p
	return p
}

// cachedFont returns walk's shared Font without disposing it (walk.NewFont
// caches fonts in a global map, so callers must never Dispose() the result).
func cachedFont(family string, pt int, style walk.FontStyle) *walk.Font {
	paintCache.mu.Lock()
	defer paintCache.mu.Unlock()
	if paintCache.fonts == nil {
		paintCache.fonts = make(map[fontKey]*walk.Font)
	}
	k := fontKey{family: family, pt: pt, style: style}
	if f, ok := paintCache.fonts[k]; ok {
		return f
	}
	f, err := walk.NewFont(family, pt, style)
	if err != nil || f == nil {
		return nil
	}
	paintCache.fonts[k] = f
	return f
}
