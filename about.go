package main

import (
	"fmt"

	"github.com/lxn/walk"
)

func showAbout(owner walk.Form) {
	walk.MsgBox(owner, "About "+appName, fmt.Sprintf("%s\r\n\r\nFilm scan tools for DaVinci Resolve: film base, white balance, tethered HDR scanning, RAW to linear TIFF, crop, and Convert (digital RAW to simulated colour negative).\r\n\r\nFormerly %s (up to v0.67). Settings and the film base DB of %s are copied once on first start; the old folder is left unchanged.\r\n\r\nSettings folder: %s\r\nCrash log: %s\r\n\r\nNegative-to-positive grading for scanned film remains in the independent NegativeScan Studio OFX plugin. Convert simulates exposing a digital RAW onto a virtual colour negative.\r\nThe embedded Kodak 2383 LUTs come from the user's own DaVinci Resolve Film Looks files.", appVersion, legacyAppName, legacyAppName, configDir(), crashFile()), walk.MsgBoxIconInformation)
}
