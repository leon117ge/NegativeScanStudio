package hikcam

import (
	"os"
	"strings"
	"testing"
)

// The macOS loader (mvs_darwin.go / mvs_darwin.c) is a copy of the Windows
// loader with a different library loader. mvs_windows.* must stay untouched,
// so these tests keep the copies from drifting: any fix to the Windows
// backend has to be carried over to macOS (or listed here on purpose).

func readSrc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestDarwinGoMirrorsWindows(t *testing.T) {
	win := readSrc(t, "mvs_windows.go")
	mac := readSrc(t, "mvs_darwin.go")
	subs := [][2]string{
		{"//go:build windows\n", "//go:build darwin && cgo\n\n// Mirror of mvs_windows.go for macOS (MvCameraControl.framework loaded with\n// dlopen by mvs_darwin.c). Keep the two files in step: mvs_mirror_test.go\n// fails if they differ outside the lines listed there.\n"},
		{"#cgo LDFLAGS: -lkernel32\n", "#cgo LDFLAGS: -ldl\n"},
		{"// SDKAvailable reports whether MvCameraControl.dll is loaded.", "// SDKAvailable reports whether MvCameraControl.framework is loaded."},
		{`return "Hikrobot MVS SDK loaded (MvCameraControl.dll)"`, `return "Hikrobot MVS SDK loaded (MvCameraControl.framework)"`},
		{"// Enumerate lists USB3 and GigE MVS cameras. It never panics if the DLL", "// Enumerate lists USB3 and GigE MVS cameras. It never panics if the SDK"},
	}
	want := win
	for _, s := range subs {
		if strings.Count(want, s[0]) != 1 {
			t.Fatalf("mvs_windows.go: expected exactly one %q", s[0])
		}
		want = strings.Replace(want, s[0], s[1], 1)
	}
	if want != mac {
		t.Fatal("mvs_darwin.go no longer mirrors mvs_windows.go; port the change or update the list in this test")
	}
}

// section returns src from the first occurrence of from up to (not including) to.
func section(t *testing.T, name, src, from, to string) string {
	t.Helper()
	i := strings.Index(src, from)
	if i < 0 {
		t.Fatalf("%s: %q not found", name, from)
	}
	j := strings.Index(src[i:], to)
	if j < 0 {
		t.Fatalf("%s: %q not found after %q", name, to, from)
	}
	return src[i : i+j]
}

func TestDarwinCMirrorsWindows(t *testing.T) {
	win := readSrc(t, "mvs_windows.c")
	mac := readSrc(t, "mvs_darwin.c")
	// SDK struct copies (layout must match the SDK headers on both OSes).
	ws := section(t, "mvs_windows.c", win, "#define MV_MAX_DEVICE_NUM", "typedef int (__stdcall *FN_EnumDevices)")
	ms := section(t, "mvs_darwin.c", mac, "#define MV_MAX_DEVICE_NUM", "typedef int (*FN_EnumDevices)")
	// The only intended difference: MV_FRAME_OUT's enPixelType is 64-bit
	// on macOS (see the comment in mvs_darwin.c).
	winFrame := "    unsigned int enPixelType;\n"
	macFrame := "    uint64_t enPixelType;\n"
	if strings.Count(ws, winFrame) != 1 || strings.Count(ms, macFrame) != 1 {
		t.Fatal("MV_FRAME_OUT enPixelType field not found where expected")
	}
	ms = ms[:strings.Index(ms, "/* MV_FRAME_OUT.")] + ms[strings.Index(ms, "typedef struct {\n    unsigned char *pBufAddr;"):]
	if strings.Replace(ws, winFrame, macFrame, 1) != ms {
		t.Error("SDK struct definitions differ between mvs_windows.c and mvs_darwin.c")
	}
	// Function pointer types: identical apart from __stdcall.
	wf := section(t, "mvs_windows.c", win, "typedef int (__stdcall *FN_EnumDevices)", "static HMODULE hMVS;")
	mf := section(t, "mvs_darwin.c", mac, "typedef int (*FN_EnumDevices)", "typedef int (*FN_Initialize)(void);")
	if strings.ReplaceAll(wf, "__stdcall ", "") != mf {
		t.Error("SDK function pointer types differ between mvs_windows.c and mvs_darwin.c")
	}
	// Everything after the loader: enum, device info, open, nodes, grab.
	wb := section(t, "mvs_windows.c", win, "int hik_available(void)", "#else\nint hik_load(void) { return -1; }")
	mb := mac[strings.Index(mac, "int hik_available(void)"):]
	wb = strings.Replace(wb, "*pixfmt = gFrame.enPixelType;", "*pixfmt = (unsigned int)gFrame.enPixelType;", 1)
	if wb != mb {
		t.Error("hik_* bodies after hik_load differ between mvs_windows.c and mvs_darwin.c")
	}
	wc := section(t, "mvs_windows.c", win, "static void copy_c(", "int hik_load(void) {")
	mc := section(t, "mvs_darwin.c", mac, "static void copy_c(", "int hik_load(void) {")
	if wc != mc {
		t.Error("copy_c differs between mvs_windows.c and mvs_darwin.c")
	}
}
