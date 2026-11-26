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
