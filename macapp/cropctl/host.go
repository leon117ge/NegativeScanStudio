// Package cropctl is the toolkit-free part of the macOS Crop and Batch RAW
// to TIFF pages: the Windows CropUI and RAWUI (../../croppage.go,
// ../../rawpage.go) with their widgets replaced by plain fields the Gio view
// draws. Image work runs on worker goroutines; everything else runs on the
// window goroutine through Host.Post, like walk's Synchronize on Windows.
package cropctl

import "basetool/macapp/appstate"

// Host is what the controllers need from the UI.
type Host interface {
	// Post runs f on the UI goroutine and redraws (never blocks the caller).
	Post(f func())
	// Message shows a message box (non-blocking).
	Message(title, text string)
	// PickFiles is a native open panel with multiple selection; done runs
	// on the UI goroutine.
	PickFiles(title, start string, exts []string, done func(paths []string, ok bool))
	// PickFolder is a native folder panel; done runs on the UI goroutine.
	PickFolder(title, start string, done func(path string, ok bool))
}

const appVersion = appstate.AppVersion

func configDir() string { return appstate.ConfigDir() }

// Item lists of the Crop page, in Windows display order.
var (
	AspectNames = cropAspectNames
	InsetNames  = cropInsetNames
	GridNames   = cropGridNames
	JPEGQNames  = cropJPEGQNames
)

// Geom is the preview geometry (pvGeom).
type Geom = pvGeom
