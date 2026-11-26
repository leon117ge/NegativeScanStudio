#!/usr/bin/env python3
"""Compare two cpu_parity output folders (for example Windows x64 reference vs macOS arm64).

Gate (per float32 value): both NaN, or equal infinities, or
|a - b| <= ABS_TOL + REL_TOL * |reference|. Bit-identical counts, worst absolute
difference and worst ULP distance are reported for every variant.
Usage: check_cpu_parity.py <reference dir> <candidate dir> [report.json]
Standard library only (runs on stock Windows, macOS and Linux Python 3).
"""
import array, hashlib, json, math, pathlib, struct, sys

ABS_TOL = 1e-5
REL_TOL = 1e-5

def load(path):
    a = array.array('f')
    a.frombytes(path.read_bytes())
    if sys.byteorder != 'little':
        a.byteswap()
    return a

def ulp_key(x):
    i = struct.unpack('<i', struct.pack('<f', x))[0]
    return i if i >= 0 else -2147483648 - i

def main():
    ref, cand = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
    names = [l.split()[0] for l in (ref / 'manifest.txt').read_text().splitlines()[1:] if l.strip()]
    cand_names = [l.split()[0] for l in (cand / 'manifest.txt').read_text().splitlines()[1:] if l.strip()]
    if names != cand_names:
        print('FAIL: variant lists differ', names, cand_names); return 1
    report, failed, worst_all = [], 0, 0.0
    for name in names:
        rb, cb = (ref / f'{name}.f32').read_bytes(), (cand / f'{name}.f32').read_bytes()
        a, b = load(ref / f'{name}.f32'), load(cand / f'{name}.f32')
        if len(a) != len(b):
            print(f'FAIL {name}: sizes differ'); failed += 1; continue
        same = diff_abs = worst_ulp = bad = 0
        worst_at = -1
        if rb == cb:
            same = len(a)
        else:
            for i, (x, y) in enumerate(zip(a, b)):
                if x == y or (math.isnan(x) and math.isnan(y)):
                    same += 1; continue
                if math.isinf(x) or math.isinf(y) or math.isnan(x) or math.isnan(y):
                    bad += 1; continue
                d = abs(x - y)
                if d > diff_abs:
                    diff_abs, worst_at = d, i
                worst_ulp = max(worst_ulp, abs(ulp_key(x) - ulp_key(y)))
                if d > ABS_TOL + REL_TOL * abs(x):
                    bad += 1
        row = dict(variant=name, values=len(a), bit_identical=same,
                   bit_identical_fraction=same / len(a), max_abs_diff=diff_abs,
                   max_ulp=worst_ulp, worst_index=worst_at, out_of_tolerance=bad,
                   sha256_reference=hashlib.sha256(rb).hexdigest(),
                   sha256_candidate=hashlib.sha256(cb).hexdigest())
        report.append(row)
        worst_all = max(worst_all, diff_abs)
        failed += bad > 0
        print(f"{'PASS' if bad == 0 else 'FAIL'} {name}: identical {same}/{len(a)} "
              f"max_abs {diff_abs:.3g} max_ulp {worst_ulp} out_of_tol {bad} sha_equal {rb == cb}")
    summary = dict(abs_tol=ABS_TOL, rel_tol=REL_TOL, variants=len(names), failed=failed,
                   max_abs_diff=worst_all, rows=report)
    if len(sys.argv) > 3:
        pathlib.Path(sys.argv[3]).write_text(json.dumps(summary, indent=1))
    print(f"{'PASS' if failed == 0 else 'FAIL'}: {len(names)} variants, worst max_abs {worst_all:.3g}, "
          f"tolerance {ABS_TOL} + {REL_TOL} * |ref|")
    return 1 if failed else 0

if __name__ == '__main__':
    sys.exit(main())
