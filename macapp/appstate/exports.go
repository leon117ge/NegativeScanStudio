package appstate

import (
	"fmt"

	"basetool/camera"
	"basetool/hdrscan"
)

// Exported views of the copied Windows declarations, for the Gio UI and the
// HDR controller (the copies themselves stay byte-identical to Windows).

const (
	AppName    = appName
	AppVersion = appVersion
)

// Item lists in Windows display order.
var (
	TargetItems       = targetItems
	SlowestItems      = slowestItems
	Frame0Items       = frame0Items
	ISOItems          = isoItems
	FixedShutterItems = fixedShutterItems
	StockTolItems     = stockTolItems
	BracketItems      = bracketItems
	AlignItems        = alignItems
	MergeItems        = mergeItems
	AlignModes        = alignModes
	MergeModes        = mergeModes
	DustItems         = dustItems
	AreaItems         = []string{"Auto: find the base (rebate, gaps, sprocket area)", "Rectangle drawn on the image"}
	TIFFItems         = []string{"Delete (keep only the HDR)", "Keep all frame TIFFs"}
	RAFItems          = []string{"Keep all RAF files", "Delete after a good merge"}
	CompItems         = []string{"None (fastest)", "LZW + predictor (lossless)"}
)

// Values behind the item lists (shared with the Light page controller).
var (
	ISOVals          = isoVals
	TargetVals       = targetVals
	FixedShutterVals = fixedShutterVals
	FixedShutterTip  = fixedShutterTip
)

// FixedShutterIndexOf is the base shutter selection for a stored shutter (-1 = none).
func FixedShutterIndexOf(us uint32) int { return fixedShutterIndexOf(us) }

const (
	BracketDefault = bracketDefault
	MergeDefault   = mergeDefault
	SimItem        = "Simulated camera (test with a RAW file)"
)

// LoadHDRConfig / SaveHDRConfig read and write hdr_config.txt in the shared
// settings folder.
func LoadHDRConfig() HDRConfig  { return loadHDRConfig() }
func SaveHDRConfig(c HDRConfig) { saveHDRConfig(c) }
func ConfigDir() string         { return configDir() }
func DefaultOutDir() string     { return defaultOutDir() }

// Settings derived from a config, as the HDRUI helpers compute them.
func Target(c HDRConfig) float64 {
	return targetVals[clampDef(c.Target, len(targetVals), targetDefault)]
}
func Slowest(c HDRConfig) uint32 { return slowestVals[clampIdx(c.Slowest, len(slowestVals))] }
func Fixed(c HDRConfig) bool     { return c.Frame0 == 1 }
func FixedISO(c HDRConfig) int   { return isoVals[clampIdx(c.ISO, len(isoVals))] }
func FixedShutter(c HDRConfig) uint32 {
	return fixedShutterVals[clampIdx(c.Shutter, len(fixedShutterVals))]
}
func Tol(c HDRConfig) float64 {
	return stockTolVals[clampDef(c.StockTol, len(stockTolVals), stockTolDefault)]
}
func EVs(c HDRConfig) []int {
	return bracketEVs[clampDef(c.Bracket, len(bracketEVs), bracketDefault)]
}
func AlignMode(c HDRConfig) int { return alignModes[clampIdx(c.Align, len(alignModes))] }
func MergeMode(c HDRConfig) int { return mergeModes[clampDef(c.Merge, len(mergeModes), mergeDefault)] }
func EVText(evs []int) string   { return evText(evs) }

// BrighterText is the "Brighter" readout: "+2: 1/20,  +3: 1/10".
func BrighterText(e0 camera.Exposure, evs []int) string {
	s := ""
	for i, ev := range evs[1:] {
		if i > 0 {
			s += ",  "
		}
		e, err := hdrscan.PlusEV(e0, ev)
		if err != nil {
			s += fmt.Sprintf("+%d: too slow", ev)
			continue
		}
		s += fmt.Sprintf("+%d: %s", ev, camera.ShutterLabel(e.Shutter))
	}
	return s
}

func clampDef(i, n, def int) int {
	if i < 0 || i >= n {
		return def
	}
	return i
}

// MergeTip is the Merge combo tooltip.
const MergeTip = mergeTip
