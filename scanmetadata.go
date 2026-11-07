package main

import "basetool/scanmeta"

// Capture GUI settings once before a scan starts. A pending camera matrix is
// recorded, never falsely marked baked into the TIFF (NSS measures through it
// for preview/base reference but does not modify the TIFF pixels).
func (u *HDRUI) scanMetadata() scanmeta.Metadata {
	m := scanmeta.Metadata{Roll: u.cfg.Roll, Mode: "white-light", Encoding: "linear"}
	if u.lockLight != nil {
		v := *u.lockLight
		m.LightRGB = &v
		m.LightModel = "CuteNewDesign RGB Light Air"
	}
	if l := u.light; l != nil {
		values := l.cfg.Values
		if m.LightRGB != nil {
			values = *m.LightRGB
		} else if st := u.lightState(); st != nil {
			values = st.Value
		}
		m = m.WithLeveling(u.metadataLeveling(), values)
	}
	return m
}

func withScanCamera(m scanmeta.Metadata) scanmeta.Metadata {
	if p, k := activeCam(); p != nil {
		var a [9]float64
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				a[3*r+c] = p.M[r][c]
			}
		}
		m.CameraMatrix = &a
		m.CameraStrength = k
		m.CameraProfile = p.Name
	}
	return m
}
