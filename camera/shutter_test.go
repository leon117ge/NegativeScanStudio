package camera

import (
	"encoding/binary"
	"testing"
)

// xt3Enum is the X-T3's 0x500D ExposureTime list from the v0.80 field log
// (camera diagnostics, FW of the user's body), 1/8000 .. 1/1 s part.
var xt3Enum = []uint32{122, 153, 193, 244, 307, 387, 488, 615, 775, 976, 1230, 1550, 1953, 2460, 3100, 3906, 4921, 6200, 7812, 9843, 12401, 15625,
	19686, 24803, 31250, 39372, 49606, 62500, 78745, 99212, 125000, 157490, 198425, 250000, 314980, 396850, 500000, 629960, 793700, 1000000}

// propXT3 stores ISO/shutter writes like the body: a shutter outside the
// enum is refused with InvalidDevicePropValue.
type propXT3 struct {
	props  map[uint16]int64
	writes []string
}

func (m *propXT3) Exec(op uint16, params []uint32, out []byte, want bool) (uint16, []uint32, []byte, error) {
	switch op {
	case OpSetDevicePropValue:
		p := uint16(params[0])
		var v int64
		switch len(out) {
		case 2:
			v = int64(binary.LittleEndian.Uint16(out))
		case 4:
			v = int64(binary.LittleEndian.Uint32(out))
		}
		if p == PropExposureTime {
			ok := false
			for _, e := range xt3Enum {
				ok = ok || int64(e) == v
			}
			if !ok {
				return RespInvalidDevPropVal, nil, nil, nil
			}
		}
		m.props[p] = v
		m.writes = append(m.writes, ShutterLabel(uint32(v)))
		return RespOK, nil, nil, nil
	case OpGetDevicePropValue:
		p := uint16(params[0])
		switch p {
		case PropExposureTime, PropExposureIndex:
			return RespOK, nil, u32(uint32(m.props[p])), nil
		case PropExposureProgram:
			return RespOK, nil, u16(1), nil
		}
		return RespOK, nil, u16(1), nil
	}
	return RespOK, nil, nil, nil
}
func (m *propXT3) SupportedVendorOps() ([]uint16, error) { return nil, nil }
func (m *propXT3) Close() error                          { return nil }

// TestShutter1_125OnXT3 (v0.81): the "1/120 s" Fixed shutter is written as
// PTP ExposureTime 7812 (uint32 LE) and read back as 7812; the nominal
// 1/120 (8333 µs) is not in the body's list and would be refused.
func TestShutter1_125OnXT3(t *testing.T) {
	m := &propXT3{props: map[uint16]int64{PropExposureTime: 15625, PropExposureIndex: 160}}
	log := NewLogger("")
	log.OnLine = func(s string) { t.Log(s) }
	f := &Fuji{p: &PTP{T: m, Log: log}, log: log, okStrategy: -1}
	got, err := f.SetExposure(Exposure{ISO: 250, Shutter: Shutter1_125})
	if err != nil || got != (Exposure{ISO: 250, Shutter: 7812}) || m.props[PropExposureTime] != 7812 {
		t.Fatalf("1/125: %v %v props %v", got, err, m.props)
	}
	if l := ShutterLabel(Shutter1_125); l != "1/125" {
		t.Fatalf("label %q", l)
	}
	if Shutter1_125*2 != Shutter1_60+1 && Shutter1_125*2 != Shutter1_60-1 {
		t.Fatalf("1/125 is not one stop from 1/60: %d", Shutter1_125)
	}
	if _, err := f.SetExposure(Exposure{ISO: 250, Shutter: 8333}); err == nil {
		t.Fatal("8333 µs (nominal 1/120) accepted; the X-T3 list has no such value")
	}
	got, err = f.SetExposure(Exposure{ISO: 250, Shutter: Shutter1_60})
	if err != nil || got.Shutter != 15625 {
		t.Fatal(got, err)
	}
}
