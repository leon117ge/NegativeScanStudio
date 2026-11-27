//go:build darwin || (linux && gio)

package main

// Windows ToolTipText of the read-only result and log text boxes, verbatim
// (TestResultTipsMatchWindows checks them against the Windows sources).
const (
	// hdrpage.go capture result (resTE)
	hdrResTip = "[roll_]YYYYMMDD_HHMMSS_NNNN_HDR.tiff (time of frame 0; NNNN counts up in the folder), RAFs _0EV/_+3EV/..., Record base _BASE.RAF. One linear 16-bit TIFF per frame (dust-cleaned if Dust removal is on), frame 0 scale."
	// tripage.go result (resTE)
	triResTip = "RAFs <name>_R_0EV.RAF ... (same folder, roll and counter as HDR scan)."
	// monopage.go report (repTE)
	monoRepTip = "Linear 16-bit TIFF, black level subtracted, nss.scan/1 metadata. CA, HDR, settle and R/G/B movement reports appear here and in mono_capture metadata; Processing stays empty so the OFX plugin loads the scan."
	// croppage.go export log (logTE)
	cropLogTip = "Same bit depth, ICC profile and metadata kept. Existing files are never overwritten."
)
