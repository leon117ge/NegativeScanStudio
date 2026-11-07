package cropper

import (
	"runtime"
	"sync"
)

// parallelRows splits [0,n) into chunks processed concurrently.
func parallelRows(n int, fn func(y0, y1 int)) {
	w := runtime.NumCPU()
	if w > n {
		w = n
	}
	if w <= 1 {
		fn(0, n)
		return
	}
	chunk := (n + w*4 - 1) / (w * 4)
	var wg sync.WaitGroup
	next := make(chan int, w*4+1)
	for y := 0; y < n; y += chunk {
		next <- y
	}
	close(next)
	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y0 := range next {
				fn(y0, min(y0+chunk, n))
			}
		}()
	}
	wg.Wait()
}
