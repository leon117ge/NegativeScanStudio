// dustcli: offline check of the --dust path (hdrscan.DespeckleFile).
package main

import (
	"fmt"
	"os"
	"time"

	"basetool/hdrscan"
)

func main() {
	lv, ok := hdrscan.ParseDust(os.Args[1])
	if !ok {
		fmt.Println("bad level")
		return
	}
	for _, f := range os.Args[2:] {
		t := time.Now()
		out, st, err := hdrscan.DespeckleFile(f, lv, "Film Base Meter v0.65")
		fmt.Println(out, st, err, time.Since(t).Round(time.Millisecond))
	}
}
