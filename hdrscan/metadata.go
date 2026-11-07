package hdrscan

import (
	"basetool/rawconv"
	"basetool/scanmeta"
)

func writeScanTIFF(path string, w, h int, pix []uint16, lzw bool, make_, model, software string, meta scanmeta.Metadata) error {
	description, err := meta.JSON(software)
	if err != nil {
		return err
	}
	return rawconv.WriteTIFF16Description(path, w, h, pix, lzw, make_, model, software, description)
}
