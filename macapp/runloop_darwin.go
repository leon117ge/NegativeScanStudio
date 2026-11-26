//go:build darwin

package main

/*
#cgo CFLAGS: -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework CoreFoundation
void nssArmProbe(void);
*/
import "C"

import (
	"fmt"
	"time"
)

var probeCh = make(chan [2]int, 4)

//export nssProbeFired
func nssProbeFired(which, onMain C.int) {
	probeCh <- [2]int{int(which), int(onMain)}
}

// runLoopProbe arms the probe from a worker goroutine (as a camera job
// would) and waits for both callbacks, which must arrive on the main thread.
func runLoopProbe(timeout time.Duration) error {
	C.nssArmProbe()
	got := map[int]bool{}
	end := time.After(timeout)
	for len(got) < 2 {
		select {
		case p := <-probeCh:
			if p[1] == 0 {
				return fmt.Errorf("callback %d ran off the main thread", p[0])
			}
			got[p[0]] = true
		case <-end:
			return fmt.Errorf("main run loop callbacks fired: main queue %v, CFRunLoop timer %v", got[0], got[1])
		}
	}
	return nil
}
