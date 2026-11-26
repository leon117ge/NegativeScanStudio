// rgblightports is a small command-line tool for the RGB Light Air (USB
// 0483:5740) on macOS and Windows. It uses the same port and protocol code as
// the app, so it is the hardware check for the serial port.
//
//	rgblightports [flags] [command [args]]
//
// Commands (default: list):
//
//	list                     list serial ports, lights (0483:5740) first
//	info                     model, serial, hardware, firmware, protocol
//	status                   state (command 20) and the last telemetry
//	on  r|g|b|all            switch channels on (command 02)
//	off r|g|b|all            switch channels off (command 02)
//	brightness r|g|b|all N   set brightness N = 1..100 (command 01)
//	setall R G B             all channels on at R, G, B = 1..100 (command 03, confirmed with 20)
//
// Flags:
//
//	-port P   serial port (default: the first port tagged 0483:5740),
//	          e.g. /dev/cu.usbmodem205E33A1544B1 or COM5
//	-raw      print the raw serial log (every frame, hex) to stderr
//	-sim      use the built-in simulated light instead of a port
//	-probe P  same as: -port P info status (kept for old notes)
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"basetool/rgblight"
)

func main() {
	port := flag.String("port", "", "serial port (default: first light found)")
	raw := flag.Bool("raw", false, "print the raw serial log to stderr")
	sim := flag.Bool("sim", false, "use the simulated light")
	probe := flag.String("probe", "", "open this port and print info and status")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: rgblightports [-port P] [-raw] [-sim] [list | info | status | on CH | off CH | brightness CH N | setall R G B]")
		fmt.Fprintln(os.Stderr, "CH is r, g, b or all; N, R, G, B are 1..100")
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if *probe != "" {
		*port = *probe
		args = []string{"info", "status"}
	}
	if len(args) == 0 {
		args = []string{"list"}
	}
	if err := run(*port, *raw, *sim, args); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

func run(port string, raw, sim bool, args []string) error {
	if args[0] == "list" {
		return list()
	}
	var rw io.ReadWriteCloser
	if sim {
		rw = rgblight.NewSimDevice().Start()
		port = "simulated light"
	} else {
		if port == "" {
			ps, err := rgblight.ListPorts()
			if err != nil {
				return err
			}
			for _, p := range ps {
				if p.Light {
					port = p.Name
					break
				}
			}
			if port == "" {
				return errors.New("no port tagged USB 0483:5740 found; pass -port")
			}
		}
		p, err := rgblight.OpenPort(port)
		if err != nil {
			return err
		}
		rw = p
	}
	fmt.Println("port:", port)
	c := rgblight.NewClient(rw)
	if raw {
		c.OnRaw = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	c.Start()
	defer c.Close()
	for len(args) > 0 {
		n, err := command(c, args)
		if err != nil {
			return fmt.Errorf("%s: %w", args[0], err)
		}
		args = args[n:]
	}
	return nil
}

// command runs one command and returns how many args it used.
func command(c *rgblight.Client, a []string) (int, error) {
	need := func(n int) error {
		if len(a) < n+1 {
			return fmt.Errorf("needs %d argument(s)", n)
		}
		return nil
	}
	switch a[0] {
	case "info":
		in, err := c.Info()
		if err != nil {
			return 0, err
		}
		fmt.Println("info:", in)
		return 1, nil
	case "status":
		st, err := c.ReadState()
		if err != nil {
			return 0, err
		}
		fmt.Printf("state: %+v\n", st)
		if t := c.LastTelemetry(); t != nil {
			fmt.Printf("telemetry: %+v\n", *t)
		}
		return 1, nil
	case "on", "off":
		if err := need(1); err != nil {
			return 0, err
		}
		chs, err := channels(a[1])
		if err != nil {
			return 0, err
		}
		for _, ch := range chs {
			if err := c.SetEnable(ch, a[0] == "on"); err != nil {
				return 0, fmt.Errorf("%s: %w", rgblight.ChannelNames[ch], err)
			}
		}
		fmt.Printf("%s %s: ok\n", a[0], a[1])
		return 2, nil
	case "brightness":
		if err := need(2); err != nil {
			return 0, err
		}
		chs, err := channels(a[1])
		if err != nil {
			return 0, err
		}
		v, err := strconv.Atoi(a[2])
		if err != nil {
			return 0, fmt.Errorf("bad value %q", a[2])
		}
		for _, ch := range chs {
			if err := c.SetBrightness(ch, v); err != nil {
				return 0, fmt.Errorf("%s: %w", rgblight.ChannelNames[ch], err)
			}
		}
		fmt.Printf("brightness %s %d: ok\n", a[1], v)
		return 3, nil
	case "setall":
		if err := need(3); err != nil {
			return 0, err
		}
		var v [3]int
		for i := 0; i < 3; i++ {
			n, err := strconv.Atoi(a[1+i])
			if err != nil {
				return 0, fmt.Errorf("bad value %q", a[1+i])
			}
			v[i] = n
		}
		st, err := c.SetAllConfirmed([3]bool{true, true, true}, v)
		if err != nil {
			return 0, err
		}
		fmt.Printf("setall: ok, state %+v\n", st)
		return 4, nil
	}
	return 0, fmt.Errorf("unknown command (use list, info, status, on, off, brightness, setall)")
}

func channels(s string) ([]int, error) {
	switch strings.ToLower(s) {
	case "r", "red":
		return []int{rgblight.ChR}, nil
	case "g", "green":
		return []int{rgblight.ChG}, nil
	case "b", "blue":
		return []int{rgblight.ChB}, nil
	case "all":
		return []int{rgblight.ChR, rgblight.ChG, rgblight.ChB}, nil
	}
	return nil, fmt.Errorf("bad channel %q (r, g, b or all)", s)
}

func list() error {
	ps, err := rgblight.ListPorts()
	if err != nil {
		return err
	}
	fmt.Printf("%s/%s, %d serial port(s), lights first:\n", runtime.GOOS, runtime.GOARCH, len(ps))
	for _, p := range ps {
		fmt.Println("  " + p.Label())
	}
	return nil
}
