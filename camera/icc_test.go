package camera

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestICCEncodeCommand(t *testing.T) {
	b, err := encodeCommand(OpGetObjectHandles, 7, []uint32{0xFFFFFFFF, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{24, 0, 0, 0, 1, 0, 0x07, 0x10, 7, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(b, want) {
		t.Fatalf("got % X\nwant % X", b, want)
	}
	if b, _ := encodeCommand(OpGetDeviceInfo, 1, nil); len(b) != 12 {
		t.Fatalf("no-parameter command is %d bytes, want 12", len(b))
	}
	if _, err := encodeCommand(OpGetDeviceInfo, 1, make([]uint32, 6)); err == nil {
		t.Fatal("6 parameters accepted")
	}
	d := encodeData(OpSetDevicePropValue, 9, []byte{1, 2})
	if h, _ := parseHeader(d); h != (header{14, ptpContainerData, OpSetDevicePropValue, 9}) {
		t.Fatalf("data header %+v", h)
	}
}

func TestICCParseResponse(t *testing.T) {
	r := []byte{20, 0, 0, 0, 3, 0, 0x01, 0x20, 5, 0, 0, 0, 0x34, 0x12, 0, 0, 0x78, 0x56, 0, 0}
	code, tid, p, err := parseResponse(r)
	if err != nil || code != RespOK || tid != 5 || len(p) != 2 || p[0] != 0x1234 || p[1] != 0x5678 {
		t.Fatalf("code 0x%04X tid %d params %v err %v", code, tid, p, err)
	}
	// trailing bytes past the container length are not parameters
	r2 := append([]byte{}, r[:12]...)
	r2[0] = 12
	if _, _, p, _ := parseResponse(append(r2, 1, 2, 3, 4)); len(p) != 0 {
		t.Fatalf("params past length: %v", p)
	}
	if _, _, _, err := parseResponse(r[:8]); err == nil {
		t.Fatal("short response accepted")
	}
	bad := append([]byte{}, r...)
	bad[4] = ptpContainerData
	if _, _, _, err := parseResponse(bad); err == nil {
		t.Fatal("data container accepted as response")
	}
}

func TestICCStripDataHeader(t *testing.T) {
	payload := []byte{0xFF, 0xD8, 1, 2, 3}
	withHdr := encodeData(OpGetObject, 3, payload)
	if got := stripDataHeader(withHdr, OpGetObject); !bytes.Equal(got, payload) {
		t.Fatalf("header not stripped: % X", got)
	}
	if got := stripDataHeader(payload, OpGetObject); !bytes.Equal(got, payload) {
		t.Fatalf("payload changed: % X", got)
	}
	if got := stripDataHeader(withHdr, OpGetObjectInfo); !bytes.Equal(got, withHdr) {
		t.Fatal("header for another op stripped")
	}
	if got := stripDataHeader(withHdr[:len(withHdr)-1], OpGetObject); len(got) != len(withHdr)-1 {
		t.Fatal("header with a wrong length stripped")
	}
}

func openSimFuji(t *testing.T, dataInHeader, dataOutHeader bool) (*Fuji, *iccSimLayer) {
	t.Helper()
	s := newICCSimLayer(dataInHeader)
	tr := newICCTransport(s)
	tr.DataOutHeader = dataOutHeader
	log := NewLogger("")
	log.OnLine = func(l string) { t.Log(l) }
	f, err := OpenFuji(tr, log, "ICC (simulated)")
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

func checkSim(t *testing.T, s *iccSimLayer) {
	t.Helper()
	if len(s.problems) > 0 {
		t.Fatalf("container problems:\n%s", strings.Join(s.problems, "\n"))
	}
}

func TestICCFujiOverSimulatedXT3(t *testing.T) {
	for _, mode := range []struct {
		name          string
		inHdr, outHdr bool
	}{{"payload", false, false}, {"data-in header", true, false}, {"data-out header", false, true}} {
		t.Run(mode.name, func(t *testing.T) {
			f, s := openSimFuji(t, mode.inHdr, mode.outHdr)
			defer f.Close()
			i := f.Info()
			if i.Model != "X-T3" || i.Manufacturer != "Fujifilm" || i.Serial != "SIM0001" || i.Driver != "ICC (simulated)" {
				t.Fatalf("info %+v", i)
			}
			if s.props[PropFujiPCMode] != 1 || s.props[PropFujiPriority] != 2 {
				t.Fatalf("PC mode %d priority %d, want 1 and 2", s.props[PropFujiPCMode], s.props[PropFujiPriority])
			}
			if len(f.ctlEnum) != len(simCtlEnum) {
				t.Fatalf("0xD208 enum %v", f.ctlEnum)
			}
			var last []byte
			for k := 0; k < 3; k++ {
				jpg, err := f.LiveView()
				if err != nil {
					t.Fatal(err)
				}
				if jpg[0] != 0xFF || jpg[1] != 0xD8 || bytes.Equal(jpg, last) {
					t.Fatalf("frame %d not a new JPEG", k)
				}
				last = jpg
			}
			if err := f.StopLiveView(); err != nil {
				t.Fatal(err)
			}
			if s.lv {
				t.Fatal("live view still on")
			}
			dir := t.TempDir()
			p, err := f.Capture(dir, "t_")
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(p)
			if err != nil || !bytes.HasPrefix(b, []byte(SimRAFHeader)) || !strings.HasSuffix(p, "t_DSCF0001.RAF") {
				t.Fatalf("capture %s: %v", p, err)
			}
			if s.shots != 1 || len(s.objects) != 0 {
				t.Fatalf("shots %d, %d objects left on the camera", s.shots, len(s.objects))
			}
			if f.okStrategy != 0 {
				t.Fatalf("release strategy %d, want A (0)", f.okStrategy)
			}
			checkSim(t, s)
		})
	}
}

func TestICCSetExposure(t *testing.T) {
	f, s := openSimFuji(t, false, false)
	want := Exposure{ISO: 320, Shutter: Shutters[ShutterIndex(16667)+3].Value}
	got, err := f.SetExposure(want)
	if err != nil || got != want {
		t.Fatalf("got %s, %v; want %s", got, err, want)
	}
	if s.props[PropExposureIndex] != 320 {
		t.Fatalf("ISO on the camera %d", s.props[PropExposureIndex])
	}
	checkSim(t, s)
}

func TestICCSupportedVendorOps(t *testing.T) {
	tr := OpenICCSim()
	ops, err := tr.SupportedVendorOps()
	if err != nil || len(ops) != 2 || ops[0] != OpFujiGetCapturePreview || ops[1] != OpFujiSetShutterSpeed {
		t.Fatalf("%v %v", ops, err)
	}
}

type failLayer struct{ closed bool }

func (l *failLayer) SendPTP(cmd, out []byte) ([]byte, []byte, error) {
	return nil, nil, errors.New("camera disconnected")
}
func (l *failLayer) Close() error { l.closed = true; return nil }

func TestICCErrors(t *testing.T) {
	l := &failLayer{}
	tr := newICCTransport(l)
	if _, _, _, err := tr.Exec(OpGetDeviceInfo, nil, nil, true); err == nil || !strings.Contains(err.Error(), "ImageCaptureCore: camera disconnected") {
		t.Fatalf("error %v", err)
	}
	if _, err := OpenFuji(tr, NewLogger(""), "ICC"); err == nil {
		t.Fatal("OpenFuji over a dead layer succeeded")
	}
	tr.Close()
	if !l.closed {
		t.Fatal("layer not closed")
	}
	if _, _, _, err := tr.Exec(OpGetDeviceInfo, nil, nil, true); err == nil {
		t.Fatal("Exec after Close succeeded")
	}
	// a non-OK response code reaches Fuji as a PTPError
	s := newICCSimLayer(false)
	p := &PTP{T: newICCTransport(s)}
	if _, err := p.PropValue(0xD999, 4); !IsResp(err, RespDevicePropNotSupp) {
		t.Fatalf("unsupported property: %v", err)
	}
}

func TestICCStub(t *testing.T) {
	if ICCAvailable {
		t.Skip("ImageCaptureCore build: covered by mactether list in CI")
	}
	if _, err := ListICC(0); err == nil {
		t.Fatal("ListICC without ImageCaptureCore succeeded")
	}
	if _, _, err := OpenICC("", 0); err == nil {
		t.Fatal("OpenICC without ImageCaptureCore succeeded")
	}
}
