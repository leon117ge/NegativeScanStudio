// Host-free independent reference: existing Go Develop port of the DCTLs.
package main

import (
	"basetool/develop"
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 65536), 16<<20)
	cx := &develop.Context{}
	for s.Scan() {
		var j struct {
			Density, Cross, Positive map[string]float64
			Stage                    int
			Pixels                   [][3]float32
		}
		if e := json.Unmarshal(s.Bytes(), &j); e != nil {
			panic(e)
		}
		nodes := []develop.Node{develop.NewNode(develop.TDensity), develop.NewNode(develop.TMask), develop.NewNode(develop.TPositive)}
		// Use committed template defaults explicitly (Go GUI defaults may have evolved).
		defaults := []map[string]float64{{"preset": 0, "in_di": 0, "out_trans": 0, "base_r": .1933, "base_g": .1883, "base_b": .0593, "cast_on": 0, "cast_r": 0, "cast_g": 0, "cast_b": 0, "dmin_code": 95, "dens_step": .002, "full_code": 1023, "ev_stops": 0, "blk_r": 0, "blk_g": 0, "blk_b": 0, "clamp01": 1}, {"mat_preset": 0, "mask_on": 0, "bd_r": 0, "bd_g": 0, "bd_b": 0, "m00": 1, "m01": 0, "m02": 0, "m10": 0, "m11": 1, "m12": 0, "m20": 0, "m21": 0, "m22": 1, "strength": 0, "dmin_code": 95, "dens_step": .002, "full_code": 1023, "clamp01": 0}, {"out_mode": 2, "contrast": 0, "white_code": 696, "gam_r": .55, "gam_g": .635, "gam_b": .62, "expo": 1, "bal_r": 0, "bal_g": 0, "bal_b": 0, "film_wb": 1, "gray_on": 0, "gray_r": 470, "gray_g": 470, "gray_b": 470}}
		for i, m := range []map[string]float64{j.Density, j.Cross, j.Positive} {
			nodes[i].V = defaults[i]
			nodes[i].Bypass = false
			for k, v := range m {
				nodes[i].V[k] = v
			}
		}
		out := make([][3]float32, len(j.Pixels))
		for i, v := range j.Pixels {
			for n, node := range nodes {
				if n > j.Stage || (n > 0 && nodes[0].V["out_trans"] != 0) {
					break
				}
				if f := node.Compile(cx); f != nil {
					v[0], v[1], v[2] = f(v[0], v[1], v[2])
				}
			}
			out[i] = v
		}
		b, e := json.Marshal(out)
		if e != nil {
			panic(e)
		}
		fmt.Println(string(b))
	}
	if e := s.Err(); e != nil {
		panic(e)
	}
}
