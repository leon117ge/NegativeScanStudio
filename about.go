package main

import (
	"fmt"

	"github.com/lxn/walk"
)

func showAbout(owner walk.Form) {
	walk.MsgBox(owner, "About "+appName, fmt.Sprintf("%s\r\n\r\nFilm scan tools for DaVinci Resolve: film base, white balance, tethered HDR scanning, RAW to linear TIFF, crop, and Develop (negative to positive with the built-in conversion chain).\r\n\r\nFormerly %s (up to v0.67). Settings and the film base DB of %s are copied once on first start; the old folder is left unchanged.\r\n\r\nSettings folder: %s\r\nCrash log: %s\r\n\r\nResolve conversion is available in the independent NegativeScan Studio OFX plugin.\r\nThe embedded Kodak 2383 LUTs come from the user's own DaVinci Resolve Film Looks files.", appVersion, legacyAppName, legacyAppName, configDir(), crashFile()), walk.MsgBoxIconInformation)
}
