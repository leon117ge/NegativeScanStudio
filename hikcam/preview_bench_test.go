package hikcam

import "testing"

func BenchmarkDisplayPipelineOldMono12(b *testing.B) {
	w, h := CS240Width, CS240Height
	src := make([]byte, w*h*2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pix, err := unpackMono(src, w, h, gvspMono12)
		if err != nil {
			b.Fatal(err)
		}
		_ = fullGray16To8(pix, w, h)
	}
}

func BenchmarkDisplayPipelineNewMono8(b *testing.B) {
	w, h := CS240Width, CS240Height
	src := make([]byte, w*h)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PreviewGray8(src, w, h, LivePreviewMaxEdge)
	}
}
