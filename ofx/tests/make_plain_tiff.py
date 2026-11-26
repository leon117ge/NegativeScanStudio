#!/usr/bin/env python3
"""Write a tiny 16-bit RGB TIFF with no ImageDescription (an 'old scan' without NSS info).
Used by host_test as its second argument where the real 0206.tiff scan is not available (CI).
Usage: make_plain_tiff.py <out.tiff>"""
import struct, sys
w, h = 4, 4
pixels = b''.join(struct.pack('<3H', 12000 + 900 * i, 15000 + 700 * i, 9000 + 500 * i) for i in range(w * h))
entries = [(256, 3, 1, w), (257, 3, 1, h), (258, 3, 3, None), (259, 3, 1, 1), (262, 3, 1, 2),
           (273, 4, 1, None), (277, 3, 1, 3), (278, 3, 1, h), (279, 4, 1, len(pixels)), (284, 3, 1, 1)]
ifd_offset = 8
ifd_size = 2 + 12 * len(entries) + 4
bps_offset = ifd_offset + ifd_size
data_offset = bps_offset + 6
out = bytearray(b'II*\x00' + struct.pack('<I', ifd_offset))
out += struct.pack('<H', len(entries))
for tag, typ, count, value in entries:
    if tag == 258:
        value = bps_offset
    if tag == 273:
        value = data_offset
    out += struct.pack('<HHI', tag, typ, count) + (struct.pack('<HH', value, 0) if typ == 3 and count == 1 else struct.pack('<I', value))
out += struct.pack('<I', 0)
out += struct.pack('<3H', 16, 16, 16)
out += pixels
open(sys.argv[1], 'wb').write(out)
