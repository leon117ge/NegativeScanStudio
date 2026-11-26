#!/usr/bin/env python3
"""Compare two rendered frames numerically (Metal vs CPU, or Mac vs Windows).

Standard library only, so it runs on a stock Mac with the Command Line Tools
python3. Reads 8- or 16-bit RGB/RGBA TIFF (uncompressed, LZW or Deflate,
optional horizontal predictor), as written by DaVinci Resolve Deliver with
Format TIFF, Codec RGB 16 bits.

  python3 compare_frames.py reference.tif candidate.tif [--max-codes 8] [--mean-codes 0.25]

Default gate (16-bit): max abs difference <= 8 codes (1.2e-4 of full scale,
the plugin's 1e-4 parity bar plus 16-bit rounding) and mean abs difference
<= 0.25 code. Exit 0 = PASS, 1 = FAIL, 2 = cannot read.
"""
import argparse
import json
import struct
import sys
import zlib
from array import array

TYPES = {1: "B", 3: "H", 4: "I", 16: "Q"}


def lzw_decode(data):
    out = bytearray()
    table = [bytes([i]) for i in range(256)] + [b"", b""]
    width, bitpos, prev = 9, 0, None
    nbits = len(data) * 8
    while bitpos + width <= nbits:
        byte = bitpos >> 3
        chunk = int.from_bytes(data[byte:byte + 4].ljust(4, b"\0"), "big")
        code = (chunk >> (32 - width - (bitpos & 7))) & ((1 << width) - 1)
        bitpos += width
        if code == 256:
            table = table[:258]
            width, prev = 9, None
            continue
        if code == 257:
            break
        if prev is None:
            entry = table[code]
        elif code < len(table):
            entry = table[code]
            table.append(prev + entry[:1])
        else:
            entry = prev + prev[:1]
            table.append(entry)
        out += entry
        prev = entry
        if len(table) + 1 >= (1 << width) and width < 12:
            width += 1
    return bytes(out)


def read_tiff(path):
    with open(path, "rb") as f:
        buf = f.read()
    bo = {b"II": "<", b"MM": ">"}.get(buf[:2])
    if not bo or struct.unpack(bo + "H", buf[2:4])[0] != 42:
        raise ValueError("not a classic TIFF (BigTIFF and other formats are not supported)")
    ifd = struct.unpack(bo + "I", buf[4:8])[0]
    n = struct.unpack(bo + "H", buf[ifd:ifd + 2])[0]
    tags = {}
    for i in range(n):
        e = ifd + 2 + 12 * i
        tag, typ, count = struct.unpack(bo + "HHI", buf[e:e + 8])
        if typ not in TYPES:
            continue
        size = struct.calcsize(TYPES[typ]) * count
        raw = buf[e + 8:e + 8 + size] if size <= 4 else buf[struct.unpack(bo + "I", buf[e + 8:e + 12])[0]:][:size]
        tags[tag] = struct.unpack(bo + TYPES[typ] * count, raw)
    w, h = tags[256][0], tags[257][0]
    bits = tags.get(258, (1,))[0]
    spp = tags.get(277, (1,))[0]
    comp = tags.get(259, (1,))[0]
    pred = tags.get(317, (1,))[0]
    planar = tags.get(284, (1,))[0]
    if bits not in (8, 16) or spp < 3 or planar != 1:
        raise ValueError(f"need 8/16-bit chunky RGB(A); got bits={bits} samples={spp} planar={planar}")
    offs, counts = tags.get(273) or tags.get(324), tags.get(279) or tags.get(325)
    if 322 in tags:
        raise ValueError("tiled TIFF is not supported; export with strips")
    rows = tags.get(278, (h,))[0]
    data = bytearray()
    for o, c in zip(offs, counts):
        chunk = buf[o:o + c]
        if comp == 1:
            data += chunk
        elif comp == 5:
            data += lzw_decode(chunk)
        elif comp in (8, 32946):
            data += zlib.decompress(chunk)
        else:
            raise ValueError(f"TIFF compression {comp} not supported (use none, LZW or Deflate)")
    typecode = "B" if bits == 8 else "H"
    px = array(typecode)
    px.frombytes(bytes(data[:w * h * spp * (bits // 8)]))
    if bits == 16 and (bo == ">") != (sys.byteorder == "big"):
        px.byteswap()
    if pred == 2:
        stride = w * spp
        for y in range(h):
            base = y * stride
            for x in range(spp, stride):
                px[base + x] = (px[base + x] + px[base + x - spp]) & (0xFF if bits == 8 else 0xFFFF)
    _ = rows
    return w, h, spp, bits, px


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("reference")
    ap.add_argument("candidate")
    ap.add_argument("--max-codes", type=float, default=8, help="max abs difference in 16-bit codes")
    ap.add_argument("--mean-codes", type=float, default=0.25, help="mean abs difference in 16-bit codes")
    ap.add_argument("--json", help="write the result as JSON here")
    a = ap.parse_args()
    try:
        ra = read_tiff(a.reference)
        rb = read_tiff(a.candidate)
    except (OSError, ValueError, KeyError, zlib.error) as e:
        print(f"CANNOT READ: {e}")
        return 2
    (w, h, sa, bits_a, pa), (w2, h2, sb, bits_b, pb) = ra, rb
    if (w, h) != (w2, h2):
        print(f"FAIL: size differs {w}x{h} vs {w2}x{h2}")
        return 1
    scale_a = 257 if bits_a == 8 else 1
    scale_b = 257 if bits_b == 8 else 1
    maxd, total, same, worst = 0, 0, 0, (0, 0, 0)
    hist = [0] * 10
    for i in range(w * h):
        for c in range(3):
            d = abs(pa[i * sa + c] * scale_a - pb[i * sb + c] * scale_b)
            total += d
            if d == 0:
                same += 1
            hist[min(d, 9)] += 1
            if d > maxd:
                maxd, worst = d, (i % w, i // w, c)
    n = w * h * 3
    mean = total / n
    ok = maxd <= a.max_codes and mean <= a.mean_codes
    res = {"reference": a.reference, "candidate": a.candidate, "size": [w, h], "bits": [bits_a, bits_b],
           "max_abs_codes": maxd, "max_abs_fraction": maxd / 65535, "mean_abs_codes": mean,
           "identical_values_pct": 100.0 * same / n, "histogram_codes_0_to_9plus": hist,
           "worst": {"x": worst[0], "y": worst[1], "channel": "RGB"[worst[2]]},
           "limits": {"max_codes": a.max_codes, "mean_codes": a.mean_codes}, "result": "PASS" if ok else "FAIL"}
    print(json.dumps(res, indent=1))
    if a.json:
        with open(a.json, "w") as f:
            json.dump(res, f, indent=1)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
