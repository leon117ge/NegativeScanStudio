package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"basetool/hdrscan"
)

// Base uses linear full-scale units (not preview/JPEG values or light percentages).
// With a camera profile it is AFTER that exact matrix/strength, before Density_In.
type levelingRecord struct {
	Time            time.Time
	Version, Mode   string
	Box             hdrscan.Rect
	Values          [3]int
	Base            [3]float64
	Profile, Source string
	Measured        bool
}

func (r levelingRecord) text() string {
	return fmt.Sprintf("%s\r\nFilm base (linear FS): R %.5f G %.5f B %.5f\r\nRGB Light Air: R %d G %d B %d\r\n%s; %s; %s; box %.6f,%.6f,%.6f,%.6f",
		r.Time.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format(time.RFC3339), r.Base[0], r.Base[1], r.Base[2],
		r.Values[0], r.Values[1], r.Values[2], r.Mode, r.Profile, r.Source, r.Box.X0, r.Box.Y0, r.Box.X1, r.Box.Y1)
}

func appendLevelingLog(path string, r levelingRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.UseCRLF = true
	if st.Size() == 0 {
		if err = w.Write([]string{"time_Asia_Shanghai", "version", "mode", "box_x0", "box_y0", "box_x1", "box_y1", "light_R", "light_G", "light_B", "base_R", "base_G", "base_B", "camera_profile", "source"}); err != nil {
			return err
		}
	}
	row := []string{r.Time.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format(time.RFC3339), r.Version, r.Mode}
	for _, v := range []float64{r.Box.X0, r.Box.Y0, r.Box.X1, r.Box.Y1} {
		row = append(row, strconv.FormatFloat(v, 'f', 6, 64))
	}
	for _, v := range r.Values {
		row = append(row, strconv.Itoa(v))
	}
	for _, v := range r.Base {
		row = append(row, strconv.FormatFloat(v, 'f', 8, 64))
	}
	row = append(row, r.Profile, r.Source)
	if err = w.Write(row); err != nil {
		return err
	}
	w.Flush()
	if err = w.Error(); err != nil {
		return err
	}
	return f.Close()
}
