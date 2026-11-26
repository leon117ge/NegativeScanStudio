package camera

// Raw PTP over Apple ImageCaptureCore (macOS), the W6 Transport.
//
// ImageCaptureCore owns the USB connection (the system ptpcamerad daemon) and
// passes raw PTP operations through ICCameraDevice
// requestSendPTPCommand:outData:completion:. This file is the portable part:
// PTP container encoding, transaction IDs and response parsing behind the
// same Transport interface the Windows WPD path uses, so Fuji (fuji.go) runs
// unchanged on top. The Objective-C side (icc_darwin.m) only moves bytes.
// Tests drive this code through a fake pass-through layer (icc_sim.go).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
)

// PTP container types (PIMA 15740 / ISO 15740 USB still image class).
const (
	ptpContainerCommand  = 1
	ptpContainerData     = 2
	ptpContainerResponse = 3
	ptpHeaderLen         = 12
)

// ICCCamera is one camera reported by ImageCaptureCore (or the simulator).
type ICCCamera struct {
	Name      string
	Serial    string
	UUID      string // ICDevice UUIDString, used to open the device
	VendorID  int    // USB vendor ID (0x04CB = Fujifilm)
	ProductID int
	CanPTP    bool // ICCameraDeviceCanAcceptPTPCommands
}

func (c ICCCamera) String() string {
	s := fmt.Sprintf("%s [%04X:%04X]", c.Name, c.VendorID, c.ProductID)
	if c.Serial != "" {
		s += " serial " + c.Serial
	}
	if !c.CanPTP {
		s += " (no raw PTP pass-through)"
	}
	return s
}

// iccLayer is the raw pass-through: send one PTP command container plus an
// optional data-out phase, get back the response container and the data-in
// phase. icc_darwin.go implements it with ImageCaptureCore; iccSimLayer
// (icc_sim.go) implements it in Go for tests and mactether -sim.
type iccLayer interface {
	SendPTP(command, dataOut []byte) (response, dataIn []byte, err error)
	Close() error
}

// ICCTransport implements Transport on top of an iccLayer.
type ICCTransport struct {
	l   iccLayer
	mu  sync.Mutex
	tid uint32
	// DataOutHeader sends the data-out phase with a 12-byte PTP data
	// container header. Off by default (ImageCaptureCore is documented to
	// take the payload); mactether -outheader switches it on for a
	// hardware check.
	DataOutHeader bool
}

func newICCTransport(l iccLayer) *ICCTransport { return &ICCTransport{l: l} }

func putHeader(b []byte, length uint32, typ, code uint16, tid uint32) {
	binary.LittleEndian.PutUint32(b[0:], length)
	binary.LittleEndian.PutUint16(b[4:], typ)
	binary.LittleEndian.PutUint16(b[6:], code)
	binary.LittleEndian.PutUint32(b[8:], tid)
}

// encodeCommand builds a PTP command container (at most 5 parameters).
func encodeCommand(op uint16, tid uint32, params []uint32) ([]byte, error) {
	if len(params) > 5 {
		return nil, fmt.Errorf("%s: %d parameters, PTP allows at most 5", OpName(op), len(params))
	}
	b := make([]byte, ptpHeaderLen+4*len(params))
	putHeader(b, uint32(len(b)), ptpContainerCommand, op, tid)
	for i, p := range params {
		binary.LittleEndian.PutUint32(b[ptpHeaderLen+4*i:], p)
	}
	return b, nil
}

// encodeData wraps a data-out payload in a PTP data container.
func encodeData(op uint16, tid uint32, payload []byte) []byte {
	b := make([]byte, ptpHeaderLen+len(payload))
	putHeader(b, uint32(len(b)), ptpContainerData, op, tid)
	copy(b[ptpHeaderLen:], payload)
	return b
}

// header is a decoded PTP container header.
type header struct {
	Length uint32
	Type   uint16
	Code   uint16
	TID    uint32
}

func parseHeader(b []byte) (header, bool) {
	if len(b) < ptpHeaderLen {
		return header{}, false
	}
	return header{binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint16(b[4:]), binary.LittleEndian.Uint16(b[6:]), binary.LittleEndian.Uint32(b[8:])}, true
}

// parseResponse decodes a PTP response container: response code and up to 5 parameters.
func parseResponse(b []byte) (code uint16, tid uint32, params []uint32, err error) {
	h, ok := parseHeader(b)
	if !ok {
		return 0, 0, nil, fmt.Errorf("PTP response too short (%d bytes)", len(b))
	}
	if h.Type != ptpContainerResponse {
		return 0, 0, nil, fmt.Errorf("PTP response container has type %d, want %d", h.Type, ptpContainerResponse)
	}
	end := len(b)
	if int(h.Length) >= ptpHeaderLen && int(h.Length) < end {
		end = int(h.Length)
	}
	for o := ptpHeaderLen; o+4 <= end && len(params) < 5; o += 4 {
		params = append(params, binary.LittleEndian.Uint32(b[o:]))
	}
	return h.Code, h.TID, params, nil
}

// stripDataHeader returns the payload of a data-in phase. If the bytes start
// with a PTP data container header for this operation (length = total size,
// type 2, code = op), the header is removed; otherwise they are the payload.
func stripDataHeader(b []byte, op uint16) []byte {
	if h, ok := parseHeader(b); ok && h.Type == ptpContainerData && h.Code == op && int(h.Length) == len(b) {
		return b[ptpHeaderLen:]
	}
	return b
}

// Exec runs one operation (Transport).
func (t *ICCTransport) Exec(op uint16, params []uint32, dataOut []byte, wantData bool) (uint16, []uint32, []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.l == nil {
		return 0, nil, nil, errors.New("device closed")
	}
	t.tid++
	if t.tid == 0 || t.tid == 0xFFFFFFFF {
		t.tid = 1
	}
	cmd, err := encodeCommand(op, t.tid, params)
	if err != nil {
		return 0, nil, nil, err
	}
	out := dataOut
	if dataOut != nil && t.DataOutHeader {
		out = encodeData(op, t.tid, dataOut)
	}
	resp, in, err := t.l.SendPTP(cmd, out)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ImageCaptureCore: %w", err)
	}
	code, _, rp, err := parseResponse(resp)
	if err != nil {
		return 0, nil, nil, err
	}
	var data []byte
	if len(in) > 0 {
		data = stripDataHeader(in, op)
		if !wantData && len(data) > 0 {
			data = nil // no data phase requested: ignore anything returned
		}
	}
	return code, rp, data, nil
}

// SupportedVendorOps lists the vendor operations (0x9000-0x9FFF) from DeviceInfo.
func (t *ICCTransport) SupportedVendorOps() ([]uint16, error) {
	resp, _, data, err := t.Exec(OpGetDeviceInfo, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if resp != RespOK {
		return nil, &PTPError{Op: OpGetDeviceInfo, Code: resp}
	}
	info, err := ParseDeviceInfo(data)
	if err != nil {
		return nil, err
	}
	var ops []uint16
	for _, o := range info.Ops {
		if o >= 0x9000 && o <= 0x9FFF {
			ops = append(ops, o)
		}
	}
	return ops, nil
}

// Close ends the ImageCaptureCore session.
func (t *ICCTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.l == nil {
		return nil
	}
	err := t.l.Close()
	t.l = nil
	return err
}
