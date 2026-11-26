//go:build darwin && !cgo

package rgblight

func init() { enumerate = enumerateIoreg }

// EnumMethod names the enumerator compiled in (for logs and the CLI).
const EnumMethod = "ioreg"
