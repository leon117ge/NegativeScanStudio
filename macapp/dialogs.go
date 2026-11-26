//go:build darwin || (linux && gio)

package main

type pickKind int

const (
	pickOpen pickKind = iota
	pickSave
	pickFolder
)

// pickPath shows a native file dialog on its own goroutine (the panel runs
// on the main thread, see dialogs_darwin.go) and delivers the answer on the
// window goroutine. The window keeps drawing while the panel is open.
func (u *UI) pickPath(title string, kind pickKind, start string, exts []string, done func(string, bool)) {
	go func() {
		p, ok := nativePick(title, kind, start, exts)
		u.Post(func() { done(p, ok) })
	}()
}
