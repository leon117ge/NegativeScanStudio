module basetool/macapp

go 1.27.3

require (
	basetool v0.0.0
	gioui.org v0.10.3
	golang.org/x/image v0.47.0
)

require (
	gioui.org/shader v1.0.9 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace basetool => ../

replace github.com/lxn/walk => ../third_party/walk
