package camera

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Transport runs raw PTP transactions inside an already-open session.
type Transport interface {
	// Exec runs one operation. If dataOut != nil it is sent host→device;
	// if wantData the device→host data phase is returned.
	Exec(op uint16, params []uint32, dataOut []byte, wantData bool) (resp uint16, rparams []uint32, data []byte, err error)
	SupportedVendorOps() ([]uint16, error)
	Close() error
}

// PTP operation codes.
const (
	OpGetDeviceInfo         = 0x1001
	OpGetStorageIDs         = 0x1004
	OpGetStorageInfo        = 0x1005
	OpGetNumObjects         = 0x1006
	OpGetObjectHandles      = 0x1007
	OpGetObjectInfo         = 0x1008
	OpGetObject             = 0x1009
	OpDeleteObject          = 0x100B
	OpInitiateCapture       = 0x100E
	OpTerminateOpenCapture  = 0x1018
	OpGetDevicePropDesc     = 0x1014
	OpGetDevicePropValue    = 0x1015
	OpSetDevicePropValue    = 0x1016
	OpInitiateOpenCapture   = 0x101C
	OpFujiGetCapturePreview = 0x9022
	OpFujiSetShutterSpeed   = 0x902C
)

// PTP response codes.
const (
	RespOK                  = 0x2001
	RespGeneralError        = 0x2002
	RespOperationNotSupp    = 0x2005
	RespInvalidObjectHandle = 0x2009
	RespDevicePropNotSupp   = 0x200A
	RespAccessDenied        = 0x200F
	RespDeviceBusy          = 0x2019
	RespInvalidDevPropVal   = 0x201C
)

// Device property codes used here.
const (
	PropFNumber          = 0x5007
	PropExposureTime     = 0x500D
	PropExposureIndex    = 0x500F
	PropExposureProgram  = 0x500E
	PropFujiPriority     = 0xD207
	PropFujiCaptureCtl   = 0xD208
	PropFujiAFStatus     = 0xD209
	PropFujiCurrentState = 0xD212
	PropFujiFreeSDRAM    = 0xD20E
	PropFujiMediaRecord  = 0xD20C
	PropFujiPCMode       = 0xD38C
	PropFujiBattery      = 0xD242
	PropFujiFirmware     = 0xD153
	PropFujiShotCount    = 0xD154
	PropFujiLVSize       = 0xD174
	PropFujiLVQuality    = 0xD173
	PropFujiISO          = 0xD02A
	PropFujiShutterOld   = 0xD219
	PropFujiAperture     = 0xD218
	PropFujiTetherRaw    = 0xD186
)

const (
	FmtAssociation = 0x3001
	FmtJPEG        = 0x3801
	VendorFuji     = 0x0000000E
)

// PTPError is a non-OK response code.
type PTPError struct {
	Op   uint16
	Code uint16
}

func (e *PTPError) Error() string {
	return fmt.Sprintf("%s returned %s", OpName(e.Op), RespName(e.Code))
}

// IsResp reports whether err is a PTP response with the given code.
func IsResp(err error, code uint16) bool {
	var pe *PTPError
	return errors.As(err, &pe) && pe.Code == code
}

var opNames = map[uint16]string{
	0x1001: "GetDeviceInfo", 0x1002: "OpenSession", 0x1003: "CloseSession", 0x1004: "GetStorageIDs",
	0x1005: "GetStorageInfo", 0x1006: "GetNumObjects", 0x1007: "GetObjectHandles", 0x1008: "GetObjectInfo",
	0x1009: "GetObject", 0x100A: "GetThumb", 0x100B: "DeleteObject", 0x100C: "SendObjectInfo",
	0x100D: "SendObject", 0x100E: "InitiateCapture", 0x100F: "FormatStore", 0x1010: "ResetDevice",
	0x1011: "SelfTest", 0x1012: "SetObjectProtection", 0x1013: "PowerDown", 0x1014: "GetDevicePropDesc",
	0x1015: "GetDevicePropValue", 0x1016: "SetDevicePropValue", 0x1017: "ResetDevicePropValue",
	0x1018: "TerminateOpenCapture", 0x1019: "MoveObject", 0x101A: "CopyObject", 0x101B: "GetPartialObject",
	0x101C: "InitiateOpenCapture",
	0x900C: "FUJI_SendObjectInfo", 0x900D: "FUJI_SendObject2", 0x901D: "FUJI_SendObject",
	0x9020: "FUJI_InitiateMovieCapture", 0x9022: "FUJI_GetCapturePreview", 0x902B: "FUJI_GetDeviceInfo",
	0x902C: "FUJI_SetShutterSpeed",
	0x9801: "MTP_GetObjectPropsSupported", 0x9802: "MTP_GetObjectPropDesc", 0x9803: "MTP_GetObjectPropValue",
	0x9804: "MTP_SetObjectPropValue", 0x9805: "MTP_GetObjPropList",
}

func OpName(op uint16) string {
	if n, ok := opNames[op]; ok {
		return fmt.Sprintf("%s(0x%04X)", n, op)
	}
	return fmt.Sprintf("0x%04X", op)
}

var respNames = map[uint16]string{
	0x2001: "OK", 0x2002: "GeneralError", 0x2003: "SessionNotOpen", 0x2004: "InvalidTransactionID",
	0x2005: "OperationNotSupported", 0x2006: "ParameterNotSupported", 0x2007: "IncompleteTransfer",
	0x2008: "InvalidStorageID", 0x2009: "InvalidObjectHandle", 0x200A: "DevicePropNotSupported",
	0x200B: "InvalidObjectFormatCode", 0x200C: "StoreFull", 0x200D: "ObjectWriteProtected",
	0x200E: "StoreReadOnly", 0x200F: "AccessDenied", 0x2010: "NoThumbnailPresent", 0x2011: "SelfTestFailed",
	0x2012: "PartialDeletion", 0x2013: "StoreNotAvailable", 0x2014: "SpecificationByFormatUnsupported",
	0x2015: "NoValidObjectInfo", 0x2016: "InvalidCodeFormat", 0x2017: "UnknownVendorCode",
	0x2018: "CaptureAlreadyTerminated", 0x2019: "DeviceBusy", 0x201A: "InvalidParentObject",
	0x201B: "InvalidDevicePropFormat", 0x201C: "InvalidDevicePropValue", 0x201D: "InvalidParameter",
	0x201E: "SessionAlreadyOpened", 0x201F: "TransactionCancelled",
}

func RespName(c uint16) string {
	if n, ok := respNames[c]; ok {
		return fmt.Sprintf("%s(0x%04X)", n, c)
	}
	return fmt.Sprintf("0x%04X", c)
}

var propNames = map[uint16]string{
	0x5001: "BatteryLevel", 0x5003: "ImageSize", 0x5004: "CompressionSetting", 0x5005: "WhiteBalance",
	0x5007: "FNumber", 0x5008: "FocalLength", 0x500A: "FocusMode", 0x500B: "ExposureMeteringMode",
	0x500C: "FlashMode", 0x500D: "ExposureTime", 0x500E: "ExposureProgramMode", 0x500F: "ExposureIndex(ISO)",
	0x5010: "ExposureBiasCompensation", 0x5011: "DateTime", 0x5012: "CaptureDelay", 0x5013: "StillCaptureMode",
	0x5018: "FocusMeteringMode",
	0xD001: "FUJI_FilmSimulation", 0xD02A: "FUJI_ExposureIndex", 0xD153: "FUJI_FirmwareVersion",
	0xD154: "FUJI_ShotCount", 0xD15D: "FUJI_SetUSBMode", 0xD173: "FUJI_LiveViewQuality",
	0xD174: "FUJI_LiveViewImageSize", 0xD186: "FUJI_TetherRawConditionCode", 0xD207: "FUJI_PriorityMode",
	0xD208: "FUJI_AFAction/CaptureControl", 0xD209: "FUJI_AFStatus", 0xD20C: "FUJI_MediaRecord",
	0xD20E: "FUJI_FreeSDRAMImages", 0xD212: "FUJI_CurrentState(events)", 0xD218: "FUJI_Aperture",
	0xD219: "FUJI_ShutterSpeed", 0xD242: "FUJI_BatteryLevel", 0xD38C: "FUJI_PCMode",
}

func PropName(p uint16) string {
	if n, ok := propNames[p]; ok {
		return fmt.Sprintf("%s(0x%04X)", n, p)
	}
	if n, ok := fujiPropNames[p]; ok {
		return fmt.Sprintf("%s(0x%04X)", n, p)
	}
	return fmt.Sprintf("0x%04X", p)
}

// ---- little-endian dataset reader ----

type rd struct {
	b   []byte
	err error
}

func (r *rd) need(n int) bool {
	if r.err != nil {
		return false
	}
	if len(r.b) < n {
		r.err = errors.New("short PTP dataset")
		return false
	}
	return true
}
func (r *rd) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}
func (r *rd) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b)
	r.b = r.b[2:]
	return v
}
func (r *rd) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}
func (r *rd) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b)
	r.b = r.b[8:]
	return v
}
func (r *rd) str() string {
	n := int(r.u8())
	if n == 0 || !r.need(2*n) {
		return ""
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(r.b[2*i:])
	}
	r.b = r.b[2*n:]
	return strings.TrimRight(string(utf16.Decode(u)), "\x00")
}
func (r *rd) arr16() []uint16 {
	n := int(r.u32())
	if n > 100000 || !r.need(2*n) {
		r.err = errors.New("bad array")
		return nil
	}
	a := make([]uint16, n)
	for i := range a {
		a[i] = r.u16()
	}
	return a
}

// ParseDeviceInfo parses a PTP DeviceInfo dataset.
func ParseDeviceInfo(b []byte) (Info, error) {
	r := &rd{b: b}
	var i Info
	r.u16() // standard version
	i.VendorExt = r.u32()
	r.u16() // vendor ext version
	i.VendorDesc = r.str()
	r.u16() // functional mode
	i.Ops = r.arr16()
	i.Events = r.arr16()
	i.Props = r.arr16()
	i.CaptureFmts = r.arr16()
	i.ImageFmts = r.arr16()
	i.Manufacturer = r.str()
	i.Model = r.str()
	i.Version = r.str()
	i.Serial = r.str()
	return i, r.err
}

// PropDesc is a parsed DevicePropDesc dataset.
type PropDesc struct {
	Code           uint16
	Type           uint16
	Writable       bool
	Default        int64
	Current        int64
	Form           uint8 // 0 none, 1 range, 2 enum
	Min, Max, Step int64
	Enum           []int64
}

func (r *rd) val(t uint16) int64 {
	switch t {
	case 1:
		return int64(int8(r.u8()))
	case 2:
		return int64(r.u8())
	case 3:
		return int64(int16(r.u16()))
	case 4:
		return int64(r.u16())
	case 5:
		return int64(int32(r.u32()))
	case 6:
		return int64(r.u32())
	case 7:
		return int64(r.u64())
	case 8:
		return int64(r.u64())
	case 0xFFFF:
		r.str()
		return 0
	}
	r.err = fmt.Errorf("unsupported datatype 0x%04X", t)
	return 0
}

// ParsePropDesc parses a GetDevicePropDesc dataset (integer types only).
func ParsePropDesc(b []byte) (PropDesc, error) {
	r := &rd{b: b}
	var d PropDesc
	d.Code = r.u16()
	d.Type = r.u16()
	d.Writable = r.u8() == 1
	d.Default = r.val(d.Type)
	d.Current = r.val(d.Type)
	d.Form = r.u8()
	switch d.Form {
	case 1:
		d.Min, d.Max, d.Step = r.val(d.Type), r.val(d.Type), r.val(d.Type)
	case 2:
		n := int(r.u16())
		for k := 0; k < n && r.err == nil; k++ {
			d.Enum = append(d.Enum, r.val(d.Type))
		}
	}
	return d, r.err
}

// ObjectInfo is the subset of a PTP ObjectInfo dataset we need.
type ObjectInfo struct {
	Storage  uint32
	Format   uint16
	Size     uint32
	Filename string
}

func ParseObjectInfo(b []byte) (ObjectInfo, error) {
	r := &rd{b: b}
	var o ObjectInfo
	o.Storage = r.u32()
	o.Format = r.u16()
	r.u16()
	o.Size = r.u32()
	r.u16()
	r.u32()
	r.u32()
	r.u32()
	r.u32()
	r.u32()
	r.u32()
	r.u32()
	r.u32()
	r.u16()
	r.u32()
	r.u32()
	o.Filename = r.str()
	return o, r.err
}

func parseU32Array(b []byte) []uint32 {
	r := &rd{b: b}
	n := int(r.u32())
	if n > 1<<20 {
		return nil
	}
	a := make([]uint32, 0, n)
	for k := 0; k < n && r.err == nil; k++ {
		a = append(a, r.u32())
	}
	return a
}

func encodeVal(t uint16, v int64) []byte {
	switch t {
	case 1, 2:
		return []byte{byte(v)}
	case 3, 4:
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(v))
		return b
	case 7, 8:
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(v))
		return b
	default:
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(v))
		return b
	}
}

func decodeVal(t uint16, b []byte) (int64, error) {
	r := &rd{b: b}
	v := r.val(t)
	return v, r.err
}

// PTP is a thin client over a Transport.
type PTP struct {
	T   Transport
	Log *Logger
	// Verbose logs every transaction.
	Verbose bool
}

func (p *PTP) do(op uint16, params []uint32, out []byte, want bool) ([]uint32, []byte, error) {
	resp, rp, data, err := p.T.Exec(op, params, out, want)
	if p.Verbose && p.Log != nil {
		ps := make([]string, len(params))
		for i, x := range params {
			ps[i] = fmt.Sprintf("0x%X", x)
		}
		es := ""
		if err != nil {
			es = " err=" + err.Error()
		}
		p.Log.Printf("PTP %s [%s] out=%dB -> %s in=%dB%s", OpName(op), strings.Join(ps, ","), len(out), RespName(resp), len(data), es)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", OpName(op), err)
	}
	if resp != RespOK {
		return rp, data, &PTPError{Op: op, Code: resp}
	}
	return rp, data, nil
}

func (p *PTP) DeviceInfo() (Info, []byte, error) {
	_, b, err := p.do(OpGetDeviceInfo, nil, nil, true)
	if err != nil {
		return Info{}, nil, err
	}
	i, err := ParseDeviceInfo(b)
	return i, b, err
}

func (p *PTP) PropDesc(code uint16) (PropDesc, error) {
	_, b, err := p.do(OpGetDevicePropDesc, []uint32{uint32(code)}, nil, true)
	if err != nil {
		return PropDesc{}, err
	}
	return ParsePropDesc(b)
}

func (p *PTP) PropRaw(code uint16) ([]byte, error) {
	_, b, err := p.do(OpGetDevicePropValue, []uint32{uint32(code)}, nil, true)
	return b, err
}

func (p *PTP) PropValue(code, typ uint16) (int64, error) {
	b, err := p.PropRaw(code)
	if err != nil {
		return 0, err
	}
	return decodeVal(typ, b)
}

func (p *PTP) SetProp(code, typ uint16, v int64) error {
	_, _, err := p.do(OpSetDevicePropValue, []uint32{uint32(code)}, encodeVal(typ, v), false)
	return err
}

func (p *PTP) ObjectHandles() ([]uint32, error) {
	_, b, err := p.do(OpGetObjectHandles, []uint32{0xFFFFFFFF, 0, 0}, nil, true)
	if err != nil {
		return nil, err
	}
	return parseU32Array(b), nil
}

func (p *PTP) StorageIDs() ([]uint32, error) {
	_, b, err := p.do(OpGetStorageIDs, nil, nil, true)
	if err != nil {
		return nil, err
	}
	return parseU32Array(b), nil
}

func (p *PTP) ObjectInfo(h uint32) (ObjectInfo, error) {
	_, b, err := p.do(OpGetObjectInfo, []uint32{h}, nil, true)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ParseObjectInfo(b)
}

func (p *PTP) Object(h uint32) ([]byte, error) {
	_, b, err := p.do(OpGetObject, []uint32{h}, nil, true)
	return b, err
}

func (p *PTP) Delete(h uint32) error {
	_, _, err := p.do(OpDeleteObject, []uint32{h, 0}, nil, false)
	return err
}

func (p *PTP) Simple(op uint16, params ...uint32) error {
	_, _, err := p.do(op, params, nil, false)
	return err
}
