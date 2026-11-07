module basetool

go 1.27.3

require (
	github.com/lxn/walk v0.0.0-20210112085537-c389da54e794
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e
	golang.org/x/image v0.47.0
)

require (
	golang.org/x/sys v0.49.0 // indirect
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
)

replace github.com/lxn/walk => ./third_party/walk
