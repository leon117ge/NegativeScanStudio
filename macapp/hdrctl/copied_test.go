package hdrctl

import (
	"testing"

	"basetool/macapp/internal/drift"
)

// Every declaration in copied.go must still equal its Windows original.
func TestCopiedLightDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied.go")
	for file, names := range map[string][]string{
		"lightpage.go": {"simLightItem", "simNoAckItem", "lightConfig", "lightConfigFile", "loadLightConfig", "toScanRect", "toLightRect",
			"setAllBusyOnce", "busyOnceLight", "busyOnceLight.SetAll", "xt3Camera", "xt3Camera.Name", "xt3Camera.Mono", "xt3Camera.Prepare",
			"xt3Camera.Shoot", "valueShot", "countFiles", "clampIdx"},
		"levelcam.go":       {"camPlan", "camPlanFor", "camPlan.post", "camPlan.label", "camPlan.worstDev", "tiffBase", "f3"},
		"camprofile.go":     {"CamProfile", "CamSettings", "camMu", "camSet", "lastTriOut", "camPath", "camSettings", "saveCamSettings", "CamSettings.find", "activeCam"},
		"levelingrecord.go": {"levelingRecord", "levelingRecord.text", "appendLevelingLog", "appendLevelingFailure", "acceptedLeveling"},
		"levelhik.go": {"camSrcExisting", "camSrcHik", "camSrcHikSim", "camSrcItems", "camProfileMonoNote", "camProfileBayerShort", "camProfileMonoShort",
			"hikLevelCamera", "hikLevelCamera.Name", "hikLevelCamera.Mono", "hikLevelCamera.GetExposure", "hikLevelCamera.scanSettings",
			"hikLevelCamera.applyScan", "hikLevelCamera.SetExposure", "hikLevelCamera.Prepare", "hikLevelCamera.Shoot", "hikTiffBase", "previewGray"},
		"configpage.go": {"presetNone", "presetSummary"},
		"castpage.go":   {"castModel"},
		"tripage.go":    {"baseHandoff"},
		"focuspage.go":  {"liveGrabMessage"},
	} {
		if file == "lightpage.go" {
			// clampIdx lives in aliases.go
			o := drift.Decls(t, "aliases.go")
			drift.Same(t, o, drift.Decls(t, "../../lightpage.go"), "clampIdx")
			names = names[:len(names)-1]
		}
		drift.Same(t, ours, drift.Decls(t, "../../"+file), names...)
	}
}

// Every declaration in copied_crop.go must still equal its Windows original.
func TestCopiedHairDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied_crop.go")
	for file, names := range map[string][]string{
		"hairpage.go":      {"hairModes", "hairNoteRE", "newer"},
		"hairview.go":      {"hairLUT", "hairPreview", "fitRect"},
		"levelmetadata.go": {"levelMetadataSummary"},
	} {
		drift.Same(t, ours, drift.Decls(t, "../../"+file), names...)
	}
}

// Every declaration in copied_tri.go must still equal its Windows original.
func TestCopiedFocusDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied_focus.go")
	drift.Same(t, ours, drift.Decls(t, "../../focuspage.go"), "zoomItems", "zoomVals", "imageToGray16", "gray16Pix")
	drift.Same(t, drift.Decls(t, "copied.go"), drift.Decls(t, "../../focuspage.go"), "liveGrabMessage")
}

func TestCopiedTrichromeDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied_tri.go")
	for file, names := range map[string][]string{
		"tripage.go":       {"triModeItems", "triModeTip", "dropShot", "simLightGain"},
		"levelmetadata.go": {"levelMetadataTip"},
		"scanmetadata.go":  {"withScanCamera"},
		"monopage.go":      {"applyLiveSettings", "monoHDRTag"},
		"monoopts_ui.go":   {"framesPerExpMin", "framesPerExpMax", "clampFloat"},
	} {
		drift.Same(t, ours, drift.Decls(t, "../../"+file), names...)
	}
}

func TestCopiedChartCalDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied_cal.go")
	drift.Same(t, ours, drift.Decls(t, "../../chartcal.go"), "calTip")
	drift.Same(t, ours, drift.Decls(t, "../../presets.go"), "cleanName")
}

func TestCopiedFilmDBDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied_filmdb.go")
	drift.Same(t, ours, drift.Decls(t, "../../hdrdb.go"), "stockNone", "frame0Base", "rgbText", "entryText", "compareText", "fnText")
	drift.Same(t, ours, drift.Decls(t, "../../presets.go"), "Preset", "maxPresets", "defaultPresets", "Config", "parsePresetLine", "parsePresets", "loadConfig")
}
