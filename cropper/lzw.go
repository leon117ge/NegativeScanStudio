package cropper

// TIFF-flavoured LZW encoder (MSB-first, "early change" code-width switch),
// as written by libtiff for Compression=5.

const (
	lzwClear   = 256
	lzwEOI     = 257
	lzwFirst   = 258
	lzwMaxCode = 4094 // libtiff: CODE_MAX-1, emit Clear before the table would need 13 bits
)

type lzwEncoder struct {
	child []uint16 // (prefix<<8 | byte) -> code, 0 = empty
	used  []uint32 // keys set since last clear
	out   []byte
	acc   uint32
	nbits uint
	width uint
	next  int
}

func newLZWEncoder() *lzwEncoder {
	return &lzwEncoder{child: make([]uint16, 4096*256)}
}

func (e *lzwEncoder) emit(code int) {
	e.acc = e.acc<<e.width | uint32(code)
	e.nbits += e.width
	for e.nbits >= 8 {
		e.out = append(e.out, byte(e.acc>>(e.nbits-8)))
		e.nbits -= 8
	}
	e.acc &= (1 << e.nbits) - 1
}

func (e *lzwEncoder) reset() {
	for _, k := range e.used {
		e.child[k] = 0
	}
	e.used = e.used[:0]
	e.width = 9
	e.next = lzwFirst
}

// Encode compresses one strip and returns the encoded bytes (valid until next call).
func (e *lzwEncoder) Encode(src []byte) []byte {
	e.out = e.out[:0]
	e.acc, e.nbits = 0, 0
	e.width = 9
	e.reset()
	e.emit(lzwClear)
	if len(src) == 0 {
		e.emit(lzwEOI)
		e.flush()
		return e.out
	}
	prefix := int(src[0])
	for _, b := range src[1:] {
		k := uint32(prefix)<<8 | uint32(b)
		if c := e.child[k]; c != 0 {
			prefix = int(c)
			continue
		}
		e.emit(prefix)
		e.child[k] = uint16(e.next)
		e.used = append(e.used, k)
		e.next++
		// early change: decoder widens when next code reaches 2^width - 1
		if e.next >= 1<<e.width && e.width < 12 {
			e.width++
		}
		if e.next >= lzwMaxCode {
			e.emit(lzwClear)
			e.reset()
		}
		prefix = int(b)
	}
	e.emit(prefix)
	// The decoder adds a table entry after this code too, which may widen.
	e.next++
	if e.next >= lzwMaxCode {
		e.emit(lzwClear)
		e.width = 9
	} else if e.next >= 1<<e.width && e.width < 12 {
		e.width++
	}
	e.emit(lzwEOI)
	e.flush()
	return e.out
}

func (e *lzwEncoder) flush() {
	if e.nbits > 0 {
		e.out = append(e.out, byte(e.acc<<(8-e.nbits)))
		e.nbits = 0
		e.acc = 0
	}
}
