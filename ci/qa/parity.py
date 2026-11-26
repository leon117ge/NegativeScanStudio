#!/usr/bin/env python3
"""Cross-platform CPU parity for the NegativeScan Studio OFX plugin math.

Self-contained (Python 3 standard library only). Three subcommands:

  cases                 write the deterministic test cases (JSON lines) to stdout
  run RUNNER OUT        feed the cases to a built ofx/tests/math_runner and save
                        its float32 output as a golden/candidate JSON file
  compare GOLDEN CAND   compare a candidate (for example macOS arm64) against a
                        golden (Windows x64 MinGW build) with tolerances

The runner is ofx/tests/math_runner.cpp. It evaluates nss::Prepared::pixel, the
same per-pixel CPU math the plugin render loop uses (Prepared::rgba), from the
same headers that plugin.cpp includes.

Inputs are exact binary fractions (16-bit codes / 65536 and small dyadic
constants), so every platform parses bit-identical float32 inputs. The input
set is identified by cases_sha256 and must match exactly. Outputs are compared
with tolerances because libm (expf, logf, powf, exp2f) and FMA contraction
differ between MinGW x86-64 and Apple clang arm64.
"""
import argparse
import base64
import hashlib
import json
import math
import os
import platform
import struct
import subprocess
import sys
import zlib

FORMAT = "nss-ofx-parity/1"
DEFAULT_MAX_ABS = 1e-4   # same bar as the GPU parity tests in PLAN.md section 2
DEFAULT_MEAN_ABS = 1e-6

# ---------------------------------------------------------------- inputs


def _lcg(seed):
    state = seed & 0xFFFFFFFF
    while True:
        state = (1664525 * state + 1013904223) & 0xFFFFFFFF
        yield state >> 16  # 16-bit code


# Integer tables (no libm at generation time, so every platform builds the
# same inputs): exposure ramp round(65536 * 2^(-8x/23)) over 8 stops, and a
# per-row tint in 1/1000 so the three channels do not move together.
DENSITY_RAMP = [65536, 51496, 40464, 31795, 24984, 19631, 15426, 12121, 9524, 7484, 5881, 4621,
                3631, 2853, 2242, 1762, 1384, 1088, 855, 672, 528, 415, 326, 256]
ROW_TINT = [[1000, 1000, 900], [1097, 1000, 912], [1148, 1000, 946], [1129, 1000, 993],
            [1050, 1000, 1042], [947, 1000, 1080], [869, 1000, 1099], [853, 1000, 1094],
            [905, 1000, 1065], [1003, 1000, 1021], [1099, 1000, 972], [1148, 1000, 929],
            [1128, 1000, 904], [1048, 1000, 902], [945, 1000, 925], [868, 1000, 965]]
MASK_BASE = [53740, 30147, 17695]  # colour negative orange mask, 16-bit codes


def frame_pixels():
    """Synthetic 24 x 16 negative scan: orange mask, 8-stop ramp, noise.

    Every value is an integer code / 65536, exactly representable in float32
    and printed exactly, so all platforms parse bit-identical inputs.
    """
    noise = _lcg(20261126)
    px = []
    for y, tint in enumerate(ROW_TINT):
        for ramp in DENSITY_RAMP:
            row = []
            for c in range(3):
                level = 2621 + (62915 * ramp) // 65536          # 0.04 + 0.96 * ramp
                code = MASK_BASE[c] * tint[c] // 1000 * level // 65536
                code += next(noise) % 97 - 48
                row.append(max(0, min(65535, code)) / 65536.0)
            px.append(row)
    return px


def special_pixels():
    s = [
        [0.0, 0.0, 0.0], [1.0, 1.0, 1.0], [0.5, 0.5, 0.5],
        [-0.0625, 2.0 ** -20, 1.25],               # out of range in both directions
        [0.02740668, 0.02740668, 0.02740668],      # log/lin knee of the DI input curve
        [2.0 ** -16, 2.0 ** -16, 2.0 ** -16],      # one 16-bit code
        [65535 / 65536.0, 0.0, 0.0], [0.0, 65535 / 65536.0, 0.0], [0.0, 0.0, 65535 / 65536.0],
        [0.25, 0.125, 0.0625], [0.0625, 0.125, 0.25], [0.75, 0.375, 0.1875],
    ]
    # gray ramp, 1/32 steps
    s += [[i / 32.0] * 3 for i in range(1, 32)]
    return s


def build_cases():
    pixels = frame_pixels() + special_pixels()
    camera = [1.125, -0.0625, -0.0625, -0.03125, 1.0625, -0.03125, 0.0, -0.125, 1.125]
    cases = []

    def add(name, **kw):
        c = {"name": name, "pixels": pixels}
        c.update(kw)
        cases.append(c)

    base = {"base_r": 0.8125, "base_g": 0.4609375, "base_b": 0.2734375}  # clear film base of the frame
    # density only
    add("density_stage0", stage=0, density=dict(base), positive={})
    add("density_stage0_di", stage=0, density=dict(base, in_di=1), positive={})
    # positive output modes x contrast, tone off (0.90.11 math)
    for mode in range(5):
        for contrast in range(4):
            add(f"pos_mode{mode}_contrast{contrast}", stage=2, density=dict(base),
                positive={"out_mode": mode, "contrast": contrast})
    # gray balance on
    for mode in (0, 2):
        add(f"pos_mode{mode}_gray", stage=2, density=dict(base),
            positive={"out_mode": mode, "gray_on": 1, "gray_r": 0.25, "gray_g": 0.25, "gray_b": 0.25})
    # product default tone and tone colour (all switches on, default amounts)
    on = {"shoulder_on": 1, "toe_on": 1, "highlight_desat_on": 1, "highlight_cream_on": 1, "shadow_desat_on": 1}
    for mode in range(5):
        add(f"tone_default_mode{mode}", stage=2, density=dict(base), positive={"out_mode": mode}, tone=dict(on))
    add("tone_strong", stage=2, density=dict(base), positive={"out_mode": 0},
        tone=dict(on, shoulder_strength=0.75, toe_strength=0.5, highlight_desat=1.0, highlight_cream=1.0,
                  shadow_desat=0.75))
    add("tone_shoulder_only", stage=2, density=dict(base), positive={"out_mode": 0},
        tone={"shoulder_on": 1, "toe_on": 0})
    # non-default density controls
    for mode in (0, 3):
        add(f"density_controls_mode{mode}", stage=2,
            density=dict(base, in_di=1, blk_r=0.0001220703125, ev_stops=-0.25, clamp01=0),
            positive={"out_mode": mode, "film_wb": 0, "gam_r": 0.4375, "gam_g": 0.53125, "gam_b": 0.53125,
                      "expo": -0.5, "bal_r": 0.125})
    add("density_out_trans", stage=2, density=dict(base, out_trans=1), positive={"out_mode": 0})
    # camera matrix
    add("camera_matrix", stage=2, camera_on=1, camera=camera, density=dict(base), positive={"out_mode": 0})
    add("camera_matrix_half", stage=2, camera_on=1, camera=camera, camera_strength=0.5,
        density=dict(base), positive={"out_mode": 2}, tone=dict(on))
    return cases


def cases_text():
    lines = []
    for c in build_cases():
        lines.append(json.dumps(c, sort_keys=True, separators=(",", ":")))
    return "\n".join(lines) + "\n"

# ---------------------------------------------------------------- run


def _sha(b):
    return hashlib.sha256(b).hexdigest()


def run(runner, out, label, extra):
    text = cases_text()
    cases = build_cases()
    proc = subprocess.run([os.path.abspath(runner)], input=text.encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr.decode(errors="replace"))
        sys.exit(f"runner failed with exit code {proc.returncode}")
    rows = [line for line in proc.stdout.decode().splitlines() if line.strip()]
    if len(rows) != len(cases):
        sys.exit(f"runner printed {len(rows)} lines for {len(cases)} cases")
    blob = bytearray()
    case_sha, counts = [], []
    for c, row in zip(cases, rows):
        vals = json.loads(row)
        if len(vals) != len(c["pixels"]):
            sys.exit(f"case {c['name']}: {len(vals)} pixels out, {len(c['pixels'])} in")
        part = bytearray()
        for rgb in vals:
            for v in rgb:
                f = float("nan") if v is None else float(v)
                part += struct.pack("<f", f)
        case_sha.append(_sha(part))
        counts.append(len(vals))
        blob += part
    doc = {
        "format": FORMAT,
        "label": label,
        "host": {"system": platform.system(), "machine": platform.machine(), "python": platform.python_version()},
        "cases_sha256": _sha(text.encode()),
        "case_names": [c["name"] for c in cases],
        "pixels_per_case": counts,
        "case_sha256": case_sha,
        "values_sha256": _sha(bytes(blob)),
        "values_f32le_zlib_b64": base64.b64encode(zlib.compress(bytes(blob), 9)).decode(),
    }
    doc.update(extra)
    with open(out, "w", encoding="ascii", newline="\n") as f:
        json.dump(doc, f, indent=1, sort_keys=True)
        f.write("\n")
    print(f"{label}: {len(cases)} cases, {sum(counts)} pixels, values_sha256 {doc['values_sha256']}")

# ---------------------------------------------------------------- compare


def _load(path):
    with open(path, encoding="ascii") as f:
        doc = json.load(f)
    if doc.get("format") != FORMAT:
        sys.exit(f"{path}: unknown format {doc.get('format')!r}")
    raw = zlib.decompress(base64.b64decode(doc["values_f32le_zlib_b64"]))
    if _sha(raw) != doc["values_sha256"]:
        sys.exit(f"{path}: values_sha256 does not match the stored values")
    n = len(raw) // 4
    return doc, struct.unpack(f"<{n}f", raw)


def compare(golden, cand, max_abs_tol, mean_abs_tol, report_path):
    g, gv = _load(golden)
    c, cv = _load(cand)
    failures = []
    expected_cases = _sha(cases_text().encode())
    if g["cases_sha256"] != c["cases_sha256"]:
        failures.append("input case sets differ (cases_sha256): golden and candidate were not run on the same inputs")
    if c["cases_sha256"] != expected_cases:
        failures.append("candidate inputs differ from this script's cases; regenerate with the same parity.py")
    if g["pixels_per_case"] != c["pixels_per_case"] or len(gv) != len(cv):
        failures.append("value counts differ")
        gv, cv = gv[:min(len(gv), len(cv))], cv[:min(len(gv), len(cv))]
    names, counts = g["case_names"], g["pixels_per_case"]
    starts, s = [], 0
    for n in counts:
        starts.append(s)
        s += n * 3

    def where(i):
        k = 0
        while k + 1 < len(starts) and starts[k + 1] <= i:
            k += 1
        off = i - starts[k]
        return names[k], off // 3, "RGB"[off % 3]

    max_abs, max_i, total, n_cmp, exact, nonfinite_mismatch = 0.0, -1, 0.0, 0, 0, 0
    max_scaled = 0.0
    per_case = {}
    for i, (a, b) in enumerate(zip(gv, cv)):
        fa, fb = math.isfinite(a), math.isfinite(b)
        if not (fa and fb):
            same = (math.isnan(a) and math.isnan(b)) or a == b
            if not same:
                nonfinite_mismatch += 1
            else:
                exact += 1
            continue
        d = abs(a - b)
        if struct.pack("<f", a) == struct.pack("<f", b):
            exact += 1
        total += d
        n_cmp += 1
        max_scaled = max(max_scaled, d / max(1.0, abs(a)))
        if d > max_abs:
            max_abs, max_i = d, i
        name = where(i)[0] if d > 0 else None
        if name:
            per_case[name] = max(per_case.get(name, 0.0), d)
    mean_abs = total / max(1, n_cmp)
    bit_exact_cases = sum(1 for x, y in zip(g["case_sha256"], c["case_sha256"]) if x == y)
    if nonfinite_mismatch:
        failures.append(f"{nonfinite_mismatch} values are NaN/Inf on one side only")
    if max_abs > max_abs_tol:
        failures.append(f"max abs error {max_abs:.3e} > {max_abs_tol:.1e}")
    if mean_abs > mean_abs_tol:
        failures.append(f"mean abs error {mean_abs:.3e} > {mean_abs_tol:.1e}")
    worst = where(max_i) if max_i >= 0 else None
    rep = {
        "golden": {"file": golden, "label": g.get("label"), "values_sha256": g["values_sha256"]},
        "candidate": {"file": cand, "label": c.get("label"), "values_sha256": c["values_sha256"]},
        "cases_sha256_golden": g["cases_sha256"], "cases_sha256_candidate": c["cases_sha256"],
        "cases": len(names), "values_compared": len(gv),
        "bit_exact_values": exact, "bit_exact_fraction": exact / max(1, len(gv)),
        "bit_exact_cases": bit_exact_cases, "identical_output_hash": g["values_sha256"] == c["values_sha256"],
        "max_abs_error": max_abs, "mean_abs_error": mean_abs, "max_scaled_error": max_scaled,
        "worst": None if worst is None else {"case": worst[0], "pixel": worst[1], "channel": worst[2],
                                              "golden": gv[max_i], "candidate": cv[max_i]},
        "tolerances": {"max_abs": max_abs_tol, "mean_abs": mean_abs_tol, "inputs": "exact (sha256)",
                       "non_finite": "exact position match"},
        "per_case_max_abs": dict(sorted(per_case.items(), key=lambda kv: -kv[1])[:10]),
        "failures": failures, "passed": not failures,
    }
    text = json.dumps(rep, indent=1)
    print(text)
    if report_path:
        with open(report_path, "w", encoding="ascii") as f:
            f.write(text + "\n")
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as f:
            f.write(f"### OFX CPU parity: {rep['candidate']['label']} vs {rep['golden']['label']}\n\n")
            f.write(f"- Result: **{'PASS' if rep['passed'] else 'FAIL'}**\n")
            f.write(f"- Values: {len(gv)} over {len(names)} cases; bit-exact {exact} "
                    f"({100 * rep['bit_exact_fraction']:.2f}%), identical cases {bit_exact_cases}/{len(names)}\n")
            f.write(f"- Max abs {max_abs:.3e} (limit {max_abs_tol:.0e}), mean abs {mean_abs:.3e} "
                    f"(limit {mean_abs_tol:.0e})\n")
            if worst:
                f.write(f"- Worst: case `{worst[0]}` pixel {worst[1]} {worst[2]}\n")
            for msg in failures:
                f.write(f"- FAIL: {msg}\n")
            f.write("\n")
    return 0 if not failures else 1


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("cases")
    r = sub.add_parser("run")
    r.add_argument("runner")
    r.add_argument("out")
    r.add_argument("--label", required=True)
    r.add_argument("--meta", action="append", default=[], help="extra KEY=VALUE stored in the file")
    c = sub.add_parser("compare")
    c.add_argument("golden")
    c.add_argument("candidate")
    c.add_argument("--max-abs", type=float, default=DEFAULT_MAX_ABS)
    c.add_argument("--mean-abs", type=float, default=DEFAULT_MEAN_ABS)
    c.add_argument("--report")
    a = ap.parse_args()
    if a.cmd == "cases":
        sys.stdout.write(cases_text())
        return 0
    if a.cmd == "run":
        run(a.runner, a.out, a.label, dict(kv.split("=", 1) for kv in a.meta))
        return 0
    return compare(a.golden, a.candidate, a.max_abs, a.mean_abs, a.report)


if __name__ == "__main__":
    sys.exit(main())
