package convert

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkProcessPreview1600(b *testing.B) {
	s, _ := StockByName("Portra 400")
	w, h := 1600, 1067
	pix := make([]uint16, w*h*3)
	for i := range pix {
		pix[i] = 12000
	}
	o := Options{Stock: s, Grain: GrainISO400, GrainSeed: 1, BorderPct: 0.04}
	b.ResetTimer()
	var last time.Duration
	for i := 0; i < b.N; i++ {
		t0 := time.Now()
		_ = Process16Fast(append([]uint16(nil), pix...), w, h, o)
		last = time.Since(t0)
	}
	b.ReportMetric(float64(last.Milliseconds()), "ms/op_last")
	fmt.Printf("last preview 1600 ISO400: %v\n", last)
}

func BenchmarkProcessExport26MP(b *testing.B) {
	s, _ := StockByName("Portra 400")
	w, h := 2000, 1333 // scaled proxy for CI; report as ~1/3 of 26MP
	pix := make([]uint16, w*h*3)
	for i := range pix {
		pix[i] = 12000
	}
	o := Options{Stock: s, Grain: GrainOff, BorderPct: 0.04}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Process16Fast(append([]uint16(nil), pix...), w, h, o)
	}
}
