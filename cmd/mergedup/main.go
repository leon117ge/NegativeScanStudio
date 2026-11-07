// mergedup: Merge-files path (no exposure info) run twice next to frame 0.
package main

import (
	"fmt"
	"os"

	"basetool/hdrscan"
)

func main() {
	for i := 0; i < 2; i++ {
		res, err := hdrscan.ProcessN(os.Args[1:], hdrscan.ProcessOptions{Merge: hdrscan.DefaultOptions(), Software: "mergedup"})
		if err != nil {
			fmt.Println("ERR", err)
			return
		}
		fmt.Println("SAVED", res.HDR, "| exposure:", res.ExposureSrc)
		for _, st := range res.StatsN {
			fmt.Println("  ", st.String()[:150])
		}
	}
}
