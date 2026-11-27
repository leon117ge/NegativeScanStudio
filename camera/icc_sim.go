//go:build !windows

package camera

// A simulated X-T3 behind the ImageCaptureCore pass-through: iccSimLayer
// decodes the PTP command containers ICCTransport builds, answers them like
// the X-T3 traces in the user's logs, and returns response containers. It
// exercises the whole ICC path (container encoding, transaction IDs, data
// phases) plus Fuji's live view and capture logic without hardware. Used by
// the unit tests and by mactether -sim.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
)

// SimICCCamera is what the simulated device reports in a camera list.
var SimICCCamera = ICCCamera{Name: "X-T3 (simulated)", Serial: "SIM0001", UUID: "00000000-0000-0000-0000-00000000F0F1",
	VendorID: 0x04CB, ProductID: 0x02DD, CanPTP: true}

// SimRAFHeader starts every simulated RAW (the real RAF magic).
const SimRAFHeader = "FUJIFILMCCD-RAW 0201FF383501"

type simObject struct {
	name   string
	format uint16
	data   []byte
}

type iccSimLayer struct {
	mu           sync.Mutex
	dataInHeader bool // answer data-in phases with a 12-byte container header
	lastTID      uint32
	problems     []string // container violations seen (checked by tests)
	props        map[uint16]int64
	objects      map[uint32]simObject
	nextHandle   uint32
	shots        int
	lv           bool
	lvFrame      int
	lvServed     bool
	ops          []uint16
	closed       bool
}

func newICCSimLayer(dataInHeader bool) *iccSimLayer {
	return &iccSimLayer{dataInHeader: dataInHeader, nextHandle: 0x90000001, objects: map[uint32]simObject{},
		props: map[uint16]int64{
			PropFujiPriority: 1, PropFujiCaptureCtl: 0x0304, PropFujiAFStatus: 0, PropFujiPCMode: 0,
			PropFujiMediaRecord: 0, PropFujiFreeSDRAM: 0, 0xD201: 0,
			PropExposureProgram: 1, PropExposureIndex: 160, PropExposureTime: 16667, PropFNumber: 0, 0x500A: 1,
		}}
}

// OpenICCSim returns a Transport over the simulated ICC layer.
func OpenICCSim() *ICCTransport { return newICCTransport(newICCSimLayer(false)) }

var simCtlEnum = []int64{0x0104, 0x0200, 0x0004, 0x0304, 0x0500, 0x000C}

func simType(code uint16) uint16 {
	switch code {
	case PropExposureIndex, PropExposureTime:
		if code == PropExposureIndex {
			return 5
		}
		return 6
	}
	return 4
}

func le16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

func ptpString(s string) []byte {
	if s == "" {
		return []byte{0}
	}
	r := []rune(s)
	b := []byte{byte(len(r) + 1)}
	for _, c := range r {
		b = append(b, le16(uint16(c))...)
	}
	return append(b, 0, 0)
}

func arr16(a []uint16) []byte {
	b := le32(uint32(len(a)))
	for _, v := range a {
		b = append(b, le16(v)...)
	}
	return b
}

func simVal(t uint16, v int64) []byte {
	switch t {
	case 5, 6:
		return le32(uint32(v))
	}
	return le16(uint16(v))
}

func simDeviceInfo() []byte {
	var b []byte
	b = append(b, le16(100)...)
	b = append(b, le32(VendorFuji)...)
	b = append(b, le16(100)...)
	b = append(b, ptpString("fujifilm.co.jp: 1.0;")...)
	b = append(b, le16(0)...)
	b = append(b, arr16([]uint16{OpGetDeviceInfo, 0x1002, 0x1003, OpGetStorageIDs, OpGetStorageInfo, OpGetNumObjects,
		OpGetObjectHandles, OpGetObjectInfo, OpGetObject, OpDeleteObject, OpInitiateCapture, OpGetDevicePropDesc,
		OpGetDevicePropValue, OpSetDevicePropValue, OpTerminateOpenCapture, OpInitiateOpenCapture,
		OpFujiGetCapturePreview, OpFujiSetShutterSpeed})...)
	b = append(b, arr16([]uint16{0x4002, 0x4003, 0x4006, 0x400C, 0x400D})...)
	b = append(b, arr16([]uint16{PropFNumber, PropExposureTime, PropExposureIndex, PropExposureProgram, 0x500A,
		PropFujiPriority, PropFujiCaptureCtl, PropFujiAFStatus, PropFujiCurrentState, PropFujiPCMode})...)
	b = append(b, arr16([]uint16{0xB103, FmtJPEG})...)
	b = append(b, arr16([]uint16{0xB103, FmtJPEG})...)
	b = append(b, ptpString("Fujifilm")...)
	b = append(b, ptpString("X-T3")...)
	b = append(b, ptpString("4.10")...)
	b = append(b, ptpString("SIM0001")...)
	return b
}

func simObjectInfo(o simObject) []byte {
	b := append(le32(0x10000001), le16(o.format)...)
	b = append(b, le16(0)...)
	b = append(b, le32(uint32(len(o.data)))...)
	b = append(b, le16(0)...)
	for i := 0; i < 8; i++ {
		b = append(b, le32(0)...)
	}
	b = append(b, le16(0)...)
	b = append(b, le32(0)...)
	b = append(b, le32(0)...)
	return append(b, ptpString(o.name)...)
}

// simJPEG renders a small frame that changes every call (like sensor noise).
func simJPEG(n int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 160, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.RGBA{uint8(x + n*7), uint8(y * 2), uint8(n * 13), 255})
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70})
	return buf.Bytes()
}

func simRAF(n int) []byte {
	b := make([]byte, 4096)
	copy(b, SimRAFHeader)
	copy(b[64:], fmt.Sprintf("simulated X-T3 shot %d", n))
	return b
}

func (s *iccSimLayer) problem(format string, a ...any) {
	s.problems = append(s.problems, fmt.Sprintf(format, a...))
}

// SendPTP implements iccLayer.
func (s *iccSimLayer) SendPTP(cmd, out []byte) ([]byte, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, fmt.Errorf("session closed")
	}
	h, ok := parseHeader(cmd)
	if !ok {
		return nil, nil, fmt.Errorf("command container too short (%d bytes)", len(cmd))
	}
	if h.Type != ptpContainerCommand || int(h.Length) != len(cmd) || (len(cmd)-ptpHeaderLen)%4 != 0 || len(cmd) > ptpHeaderLen+20 {
		s.problem("bad command container % X", cmd)
	}
	if h.TID != s.lastTID+1 {
		s.problem("%s: transaction ID %d after %d", OpName(h.Code), h.TID, s.lastTID)
	}
	s.lastTID = h.TID
	var params []uint32
	for o := ptpHeaderLen; o+4 <= len(cmd); o += 4 {
		params = append(params, binary.LittleEndian.Uint32(cmd[o:]))
	}
	p := func(i int) uint32 {
		if i < len(params) {
			return params[i]
		}
		return 0
	}
	if out != nil {
		if oh, ok := parseHeader(out); ok && oh.Type == ptpContainerData && oh.Code == h.Code && int(oh.Length) == len(out) {
			out = out[ptpHeaderLen:]
		}
	}
	s.ops = append(s.ops, h.Code)
	code, rp, data := s.exec(h.Code, p, out)
	resp := make([]byte, ptpHeaderLen+4*len(rp))
	putHeader(resp, uint32(len(resp)), ptpContainerResponse, code, h.TID)
	for i, v := range rp {
		binary.LittleEndian.PutUint32(resp[ptpHeaderLen+4*i:], v)
	}
	if data != nil && s.dataInHeader {
		data = encodeData(h.Code, h.TID, data)
	}
	return resp, data, nil
}

func (s *iccSimLayer) exec(op uint16, p func(int) uint32, out []byte) (uint16, []uint32, []byte) {
	switch op {
	case OpGetDeviceInfo:
		return RespOK, nil, simDeviceInfo()
	case OpGetDevicePropDesc:
		code := uint16(p(0))
		v, ok := s.props[code]
		if !ok {
			return RespDevicePropNotSupp, nil, nil
		}
		t := simType(code)
		b := append(le16(code), le16(t)...)
		b = append(b, 1)
		b = append(b, simVal(t, v)...)
		b = append(b, simVal(t, v)...)
		if code == PropFujiCaptureCtl {
			b = append(b, 2)
			b = append(b, le16(uint16(len(simCtlEnum)))...)
			for _, e := range simCtlEnum {
				b = append(b, simVal(t, e)...)
			}
		} else {
			b = append(b, 0)
		}
		return RespOK, nil, b
	case OpGetDevicePropValue:
		code := uint16(p(0))
		if code == PropFujiCurrentState {
			b := le16(1)
			b = append(b, le16(PropFujiFreeSDRAM)...)
			return RespOK, nil, append(b, le32(0)...)
		}
		v, ok := s.props[code]
		if !ok {
			return RespDevicePropNotSupp, nil, nil
		}
		return RespOK, nil, simVal(simType(code), v)
	case OpSetDevicePropValue:
		code := uint16(p(0))
		if _, ok := s.props[code]; !ok {
			return RespDevicePropNotSupp, nil, nil
		}
		var v int64
		switch len(out) {
		case 2:
			v = int64(binary.LittleEndian.Uint16(out))
		case 4:
			v = int64(int32(binary.LittleEndian.Uint32(out)))
		default:
			s.problem("SetDevicePropValue 0x%04X: %d data bytes", code, len(out))
			return RespGeneralError, nil, nil
		}
		s.props[code] = v
		return RespOK, nil, nil
	case OpInitiateCapture:
		switch s.props[PropFujiCaptureCtl] {
		case 0x0200:
			s.props[PropFujiAFStatus] = 2
		case 0x0304, 0x0104, 0x000C:
			s.shots++
			h := s.nextHandle
			s.nextHandle++
			s.objects[h] = simObject{fmt.Sprintf("DSCF%04d.RAF", s.shots), 0xB103, simRAF(s.shots)}
			s.props[PropFujiAFStatus] = 0
		}
		return RespOK, nil, nil
	case OpInitiateOpenCapture:
		s.lv, s.lvServed = true, false
		return RespOK, nil, nil
	case OpTerminateOpenCapture:
		if !s.lv {
			return 0x2018, nil, nil // CaptureAlreadyTerminated
		}
		s.lv = false
		return RespOK, nil, nil
	case OpGetObjectHandles:
		var b []byte
		var hs []uint32
		for h := range s.objects {
			hs = append(hs, h)
		}
		b = le32(uint32(len(hs)))
		for _, h := range hs {
			b = append(b, le32(h)...)
		}
		return RespOK, nil, b
	case OpGetObjectInfo:
		h := p(0)
		if h == lvHandle {
			if !s.lv || s.lvServed {
				return RespInvalidObjectHandle, nil, nil
			}
			return RespOK, nil, simObjectInfo(simObject{"LV.JPG", FmtJPEG, make([]byte, 1)})
		}
		o, ok := s.objects[h]
		if !ok {
			return RespInvalidObjectHandle, nil, nil
		}
		return RespOK, nil, simObjectInfo(o)
	case OpGetObject:
		h := p(0)
		if h == lvHandle {
			if !s.lv {
				return RespInvalidObjectHandle, nil, nil
			}
			s.lvFrame++
			s.lvServed = true
			return RespOK, nil, simJPEG(s.lvFrame)
		}
		o, ok := s.objects[h]
		if !ok {
			return RespInvalidObjectHandle, nil, nil
		}
		return RespOK, nil, o.data
	case OpDeleteObject:
		h := p(0)
		if h == lvHandle {
			s.lvServed = false // the body renders the next frame
			return RespOK, nil, nil
		}
		if _, ok := s.objects[h]; !ok {
			return RespInvalidObjectHandle, nil, nil
		}
		delete(s.objects, h)
		return RespOK, nil, nil
	case OpGetStorageIDs:
		return RespOK, nil, append(le32(1), le32(0x10000001)...)
	}
	return RespOperationNotSupp, nil, nil
}

func (s *iccSimLayer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
