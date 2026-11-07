// illuminantcalc prints the nominal Kodak 5219 scene-illuminant model.
package main

import (
	"basetool/colorsci"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	k := flag.Float64("kelvin", 5500, "scene illuminant K (Planck approximation, 2500..10000)")
	flag.Parse()
	r, e := colorsci.Vision3Illuminant(*k)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	out := struct {
		Model    string                     `json:"model"`
		Balance  colorsci.IlluminantBalance `json:"balance"`
		Additive [3]float64                 `json:"dctl_add_density"`
	}{Model: "Kodak H-1-5219 rev3-26, approximate ECN-2 layers, Planck visible spectrum", Balance: r}
	for i := range out.Additive {
		out.Additive[i] = -r.Density[i]
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if e = enc.Encode(out); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
