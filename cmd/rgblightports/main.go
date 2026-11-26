// rgblightports lists the serial ports the app can see and tags the RGB
// Light Air (USB 0483:5740). With -probe it opens a port and asks the light
// for its info and state (same protocol code as the app).
//
//	rgblightports
//	rgblightports -probe /dev/cu.usbmodemXXXX
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"basetool/rgblight"
)

func main() {
	probe := flag.String("probe", "", "open this port and query the light")
	flag.Parse()
	ps, err := rgblight.ListPorts()
	if err != nil {
		fmt.Println("ListPorts error:", err)
		os.Exit(1)
	}
	fmt.Printf("%s/%s, %d serial port(s), lights first:\n", runtime.GOOS, runtime.GOARCH, len(ps))
	for _, p := range ps {
		fmt.Println("  " + p.Label())
	}
	if *probe == "" {
		return
	}
	p, err := rgblight.OpenPort(*probe)
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	c := rgblight.NewClient(p)
	c.Start()
	defer c.Close()
	in, err := c.Info()
	if err != nil {
		fmt.Println("info:", err)
		os.Exit(1)
	}
	fmt.Println("light:", in)
	st, err := c.ReadState()
	if err != nil {
		fmt.Println("state:", err)
		os.Exit(1)
	}
	fmt.Printf("state: %+v\n", st)
}
