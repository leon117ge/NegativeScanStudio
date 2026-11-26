package cropctl

import (
	"testing"

	"basetool/macapp/internal/drift"
)

// Every declaration in copied.go must still equal its Windows original.
func TestCopiedCropDeclsMatchWindows(t *testing.T) {
	ours := drift.Decls(t, "copied.go")
	drift.Same(t, ours, drift.Decls(t, "../../croppage.go"),
		"CropConfig", "cropAspectNames", "cropAspects", "cropInsetNames", "cropInsets", "cropGridNames", "cropJPEGQNames", "cropJPEGQ",
		"cropConfigFile", "loadCropConfig", "saveCropConfig", "ciPending", "ciAnalyzing", "ciReady", "ciExporting", "ciDone", "ciError",
		"cropItem", "cropItem.analysed", "CropModel.RowCount", "CropModel.Value", "cropDrag", "CropUI.aspect", "CropUI.inset", "CropUI.current",
		"CropUI.worker", "CropUI.analyse", "CropUI.putProxy", "CropUI.resetLocked", "CropUI.fullRect", "CropUI.autoRect",
		"CropUI.lockAspect", "CropUI.setAngle", "CropUI.settingsChanged", "ratioText", "pvGeom", "CropUI.toCanvas", "CropUI.resize",
		"sliderMax", "CropUI.outPath")
	drift.Same(t, ours, drift.Decls(t, "../../rawpage.go"),
		"RawConfig", "rawConfigFile", "loadRawConfig", "saveRawConfig", "maxRawWorkers", "rsPending", "rsWorking", "rsDone", "rsSkipped",
		"rsError", "RawRow", "RawModel.RowCount", "RawModel.Value", "RAWUI.addFolder", "RAWUI.options", "RAWUI.next", "fileSeen")
}
