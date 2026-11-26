#!/usr/bin/env python3
"""Release gate for every macOS build of NegativeScan Studio (PLAN.md item W10).

Self-contained (Python 3 standard library only). Point it at a build output:
a .ofx.bundle, a .app, a folder, a .zip (nested zips are opened) or a .dmg
(macOS only, mounted read-only with hdiutil).

Checks
  1. CJK scan: no Chinese, Japanese or Korean characters anywhere that ships:
     file and folder names, text files (docs, scripts, Info.plist, JSON),
     binary property lists, and the string sections of Mach-O binaries
     (__cstring, __ustring as UTF-16, __objc_* names, __const, Go __rodata).
  2. Plain versions: every version found must be digits and dots only (no
     -beta, -rc, -preview, letters or CJK), and all of them must agree:
     Info.plist CFBundleShortVersionString and CFBundleVersion, the OFX label
     in the binary ("NegativeScan Studio OFX X.Y.Z"), the OFX plugin version
     from OfxGetPlugin (optional --probe JSON), the Go app version string
     ("NegativeScan Studio vX.YY"), the artifact file name and --expect-version.
  3. Layout and binaries: OFX artifacts must contain the *.ofx.bundle folder
     itself (not a bare Contents/), with Contents/Info.plist and the
     CFBundleExecutable in Contents/MacOS; every Mach-O must have an arm64
     slice and a code signature (ad-hoc is reported as WARN until a
     Developer ID exists, PLAN.md Q1; unsigned is FAIL).
  4. Docs: with --repo, the changelog for the product has an entry headed with
     the version and mentions macOS; the README has macOS install notes.

Exit code 0 = all checks pass, 1 = at least one FAIL (with --mode enforce,
the default). With --mode report the verdict is printed but the exit code is
0. WARN lines never fail.
A JSON report is written with --report and a summary to GITHUB_STEP_SUMMARY.
"""
import argparse
import io
import json
import os
import plistlib
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import zipfile

# Han (with extensions and compatibility), CJK symbols and punctuation,
# Hiragana, Katakana, Bopomofo, Hangul, half/full width forms.
CJK = re.compile(
    "[\u2e80-\u2fdf\u3000-\u303f\u3040-\u30ff\u3100-\u312f\u3130-\u318f\u31a0-\u31ff"
    "\u3400-\u4dbf\u4e00-\u9fff\ua960-\ua97f\uac00-\ud7af\uf900-\ufaff\ufe30-\ufe4f"
    "\uff00-\uffef\U00020000-\U0003134f]")
PLAIN = re.compile(r"^\d+(\.\d+){1,3}$")
TEXT_EXT = {".md", ".txt", ".json", ".plist", ".strings", ".command", ".sh", ".py", ".xml", ".html",
            ".csv", ".cube", ".dctl", ".rtf", ".yml", ".yaml", ".cfg", ".ini", ".lic", ""}
OFX_LABEL = re.compile(rb"NegativeScan Studio OFX ([0-9A-Za-z.\-+_]+)")
P2383_LABEL = re.compile(rb"(?:NSS )?Print 2383(?: OFX)? ([0-9][0-9A-Za-z.\-+_]*)")
APP_LABEL = re.compile(rb"NegativeScan Studio v([0-9][0-9A-Za-z.\-+_]*)")
STRICT_SECTIONS = {"__cstring", "__ustring", "__objc_methname", "__objc_classname", "__objc_methtype",
                   "__oslogstring", "__info_plist", "__cfstring"}
LOOSE_SECTIONS = {"__const", "__rodata", "__data", "__noptrdata", "__gosymtab", "__gopclntab"}

MH_MAGIC_64 = 0xFEEDFACF
FAT_MAGIC = 0xCAFEBABE
LC_SEGMENT_64 = 0x19


class Report:
    def __init__(self):
        self.items = []

    def add(self, level, check, msg):
        self.items.append({"level": level, "check": check, "message": msg})
        print(f"{level}: [{check}] {msg}")

    @property
    def failed(self):
        return any(i["level"] == "FAIL" for i in self.items)


# ---------------------------------------------------------------- Mach-O


def macho_slices(data):
    if len(data) < 8:
        return []
    magic_be = struct.unpack(">I", data[:4])[0]
    if magic_be == FAT_MAGIC:
        n = struct.unpack(">I", data[4:8])[0]
        out = []
        for i in range(n):
            _, _, off, size, _ = struct.unpack(">iiIII", data[8 + 20 * i:28 + 20 * i])
            out.append(data[off:off + size])
        return out
    if struct.unpack("<I", data[:4])[0] == MH_MAGIC_64:
        return [data]
    return []


def macho_sections(sl):
    """Yield (segment, section, bytes) for a thin 64-bit little-endian Mach-O."""
    ncmds = struct.unpack("<I", sl[16:20])[0]
    off = 32
    for _ in range(ncmds):
        cmd, size = struct.unpack("<II", sl[off:off + 8])
        if cmd == LC_SEGMENT_64:
            segname = sl[off + 8:off + 24].rstrip(b"\0").decode("ascii", "replace")
            nsects = struct.unpack("<I", sl[off + 64:off + 68])[0]
            s = off + 72
            for _ in range(nsects):
                sect = sl[s:s + 16].rstrip(b"\0").decode("ascii", "replace")
                addr, ssize, soff = struct.unpack("<QQI", sl[s + 32:s + 52])
                flags = struct.unpack("<I", sl[s + 64:s + 68])[0]
                zerofill = (flags & 0xFF) in (0x1, 0xC, 0x12)
                if not zerofill and soff:
                    yield segname, sect, sl[soff:soff + ssize]
                s += 80
        off += size


def macho_signature(sl):
    """Return 'unsigned', 'adhoc' or 'signed' for a thin 64-bit Mach-O."""
    ncmds = struct.unpack("<I", sl[16:20])[0]
    off = 32
    for _ in range(ncmds):
        cmd, size = struct.unpack("<II", sl[off:off + 8])
        if cmd == 0x1D:  # LC_CODE_SIGNATURE
            doff, dsize = struct.unpack("<II", sl[off + 8:off + 16])
            blob = sl[doff:doff + dsize]
            count = struct.unpack(">I", blob[8:12])[0]
            adhoc, cms = False, 0
            for i in range(count):
                _, o = struct.unpack(">II", blob[12 + 8 * i:20 + 8 * i])
                magic = struct.unpack(">I", blob[o:o + 4])[0]
                if magic == 0xFADE0C02:
                    adhoc = adhoc or bool(struct.unpack(">I", blob[o + 12:o + 16])[0] & 0x2)
                elif magic == 0xFADE0B01:
                    cms = struct.unpack(">I", blob[o + 4:o + 8])[0]
            return "adhoc" if adhoc or cms <= 8 else "signed"
        off += size
    return "unsigned"


def utf8_runs(buf, min_cjk):
    """Printable UTF-8 runs that contain at least min_cjk consecutive CJK chars."""
    hits = []
    for m in re.finditer(rb"(?:[\x20-\x7e\t]|[\xc2-\xdf][\x80-\xbf]|[\xe0-\xef][\x80-\xbf]{2}|"
                         rb"[\xf0-\xf4][\x80-\xbf]{3}){2,}", buf):
        try:
            text = m.group().decode("utf-8")
        except UnicodeDecodeError:
            continue
        run = re.search(CJK.pattern + "{%d,}" % min_cjk, text)
        if run:
            hits.append((m.start(), text[:120]))
    return hits


def scan_macho(path, data, rep, rel):
    found = False
    for sl in macho_slices(data):
        cpu = struct.unpack("<i", sl[4:8])[0]
        for seg, sect, buf in macho_sections(sl):
            if sect == "__ustring":
                text = buf.decode("utf-16-le", "replace")
                if CJK.search(text):
                    rep.add("FAIL", "cjk", f"{rel} {seg},{sect}: UTF-16 string with CJK: {CJK.search(text).group()!r}")
                    found = True
                continue
            if sect in STRICT_SECTIONS:
                for off, text in utf8_runs(buf, 1):
                    rep.add("FAIL", "cjk", f"{rel} {seg},{sect}+0x{off:x}: {text!r}")
                    found = True
            elif sect in LOOSE_SECTIONS:
                # Numeric tables can look like UTF-8 by chance; demand 2+ CJK
                # characters inside a printable run before calling it text.
                for off, text in utf8_runs(buf, 2):
                    rep.add("FAIL", "cjk", f"{rel} {seg},{sect}+0x{off:x} (check, may be a numeric table): {text!r}")
                    found = True
        rep.add("INFO", "binary", f"{rel}: Mach-O slice cputype 0x{cpu & 0xffffffff:x} scanned")
    slices = macho_slices(data)
    cpus = [struct.unpack("<i", x[4:8])[0] & 0xFFFFFFFF for x in slices]
    if 0x0100000C not in cpus:
        rep.add("FAIL", "binary", f"{rel}: no arm64 slice (cputypes {[hex(c) for c in cpus]})")
    else:
        rep.add("PASS", "binary", f"{rel}: arm64 slice present")
    for x in slices:
        sig = macho_signature(x)
        if sig == "unsigned":
            rep.add("FAIL", "signature", f"{rel}: no code signature (Apple Silicon refuses to load it)")
        elif sig == "adhoc":
            rep.add("WARN", "signature", f"{rel}: ad-hoc signature (expected until a Developer ID exists; needs quarantine clearing)")
        else:
            rep.add("PASS", "signature", f"{rel}: signed with an identity (verify with codesign/spctl on macOS)")
        break
    return found


# ---------------------------------------------------------------- walking


def expand(path, work):
    """Return a folder holding the unpacked artifact."""
    if os.path.isdir(path):
        return path
    low = path.lower()
    if low.endswith(".zip"):
        dest = tempfile.mkdtemp(dir=work)
        with zipfile.ZipFile(path) as z:
            bad = z.testzip()
            if bad:
                sys.exit(f"{path}: zip CRC error in {bad}")
            z.extractall(dest)
        for root, _, files in os.walk(dest):
            for f in files:
                if f.lower().endswith(".zip"):
                    inner = os.path.join(root, f)
                    out = inner + ".unzipped"
                    with zipfile.ZipFile(inner) as z:
                        z.extractall(out)
        return dest
    if low.endswith(".dmg"):
        if sys.platform != "darwin":
            sys.exit("a .dmg can only be checked on macOS")
        mnt = tempfile.mkdtemp(dir=work)
        subprocess.run(["hdiutil", "attach", "-readonly", "-nobrowse", "-mountpoint", mnt, path], check=True)
        dest = tempfile.mkdtemp(dir=work)
        shutil.copytree(mnt, os.path.join(dest, "dmg"), symlinks=True)
        subprocess.run(["hdiutil", "detach", mnt], check=False)
        return dest
    dest = tempfile.mkdtemp(dir=work)
    shutil.copy2(path, dest)
    return dest


def collect(root, rep, versions, args):
    files = 0
    for dirpath, dirnames, filenames in os.walk(root):
        for name in dirnames + filenames:
            if CJK.search(name):
                rep.add("FAIL", "cjk", f"file name with CJK: {os.path.relpath(os.path.join(dirpath, name), root)!r}")
        for name in filenames:
            full = os.path.join(dirpath, name)
            rel = os.path.relpath(full, root)
            if os.path.islink(full):
                continue
            files += 1
            with open(full, "rb") as f:
                data = f.read()
            is_text = not (b"\0" in data[:8192] or data.startswith(b"bplist00"))
            if not is_text:
                for rx, key in ((OFX_LABEL, "ofx label in binary"), (P2383_LABEL, "2383 label in binary"),
                                (APP_LABEL, "app version in binary")):
                    for m in sorted(set(rx.findall(data))):
                        versions.append((key + f" ({rel})", m.decode("ascii", "replace").rstrip(".")))
            if macho_slices(data):
                scan_macho(full, data, rep, rel)
                continue
            if name == "Info.plist" or data.startswith(b"bplist00"):
                try:
                    pl = plistlib.loads(data)
                except Exception as e:  # noqa: BLE001
                    rep.add("FAIL", "plist", f"{rel}: cannot parse ({e})")
                    continue
                dump = json.dumps(pl, default=str, ensure_ascii=False)
                if CJK.search(dump):
                    rep.add("FAIL", "cjk", f"{rel}: property list contains CJK: {CJK.search(dump).group()!r}")
                if name == "Info.plist":
                    for k in ("CFBundleShortVersionString", "CFBundleVersion"):
                        if k in pl:
                            versions.append((f"{k} ({rel})", str(pl[k])))
                        else:
                            rep.add("FAIL", "version", f"{rel}: {k} missing")
                    if CJK.search(str(pl.get("CFBundleDevelopmentRegion", ""))):
                        rep.add("FAIL", "cjk", f"{rel}: development region is not English")
                continue
            ext = os.path.splitext(name)[1].lower()
            try:
                text = data.decode("utf-8")
            except UnicodeDecodeError:
                text = None
            if text is not None and (ext in TEXT_EXT or "\0" not in text):
                for n, line in enumerate(text.splitlines(), 1):
                    m = CJK.search(line)
                    if m:
                        rep.add("FAIL", "cjk", f"{rel}:{n}: {m.group()!r} in {line.strip()[:100]!r}")
                continue
            # other binaries (images, metallib, data): look for real text only
            for off, t in utf8_runs(data, 2):
                rep.add("WARN", "cjk", f"{rel}+0x{off:x}: possible CJK text in a binary file: {t!r}")
    return files


def check_layout(root, product, rep, label):
    if product not in ("ofx", "print2383"):
        return
    bundles = []
    for dirpath, dirnames, _ in os.walk(root):
        for d in dirnames:
            if d.endswith(".ofx.bundle"):
                bundles.append(os.path.join(dirpath, d))
    bare = os.path.isdir(os.path.join(root, "Contents"))
    if not bundles:
        rep.add("FAIL", "layout", f"{label}: no *.ofx.bundle folder" + (" (bare Contents/ at the top: the bundle folder itself was not packed)" if bare else ""))
        return
    for b in bundles:
        rel = os.path.relpath(b, root)
        plist = os.path.join(b, "Contents", "Info.plist")
        if not os.path.isfile(plist):
            rep.add("FAIL", "layout", f"{label}: {rel}/Contents/Info.plist missing")
            continue
        with open(plist, "rb") as f:
            pl = plistlib.load(f)
        exe = pl.get("CFBundleExecutable", "")
        if pl.get("CFBundlePackageType") != "BNDL":
            rep.add("FAIL", "layout", f"{label}: {rel}: CFBundlePackageType is {pl.get('CFBundlePackageType')!r}, not BNDL")
        if not exe or not os.path.isfile(os.path.join(b, "Contents", "MacOS", exe)):
            rep.add("FAIL", "layout", f"{label}: {rel}: Contents/MacOS/{exe or '?'} missing")
        elif os.path.isdir(os.path.join(b, "Contents", "Win64")):
            rep.add("FAIL", "layout", f"{label}: {rel}: has a Win64 folder in a Mac build")
        else:
            rep.add("PASS", "layout", f"{label}: {rel} has Info.plist and Contents/MacOS/{exe}")


# ---------------------------------------------------------------- docs


def check_docs(repo, product, version, rep):
    if product == "ofx":
        changelog, readme, head = os.path.join(repo, "ofx/CHANGELOG.md"), os.path.join(repo, "ofx/README.md"), version
    elif product == "app":
        changelog, readme, head = os.path.join(repo, "docs/CHANGELOG.md"), os.path.join(repo, "README.md"), "v" + version
    else:  # print2383
        changelog, readme, head = os.path.join(repo, "CHANGELOG.md"), os.path.join(repo, "README.md"), version
    for path in (changelog, readme):
        if not os.path.isfile(path):
            rep.add("FAIL", "docs", f"{os.path.relpath(path, repo)} missing")
            return
    text = open(changelog, encoding="utf-8").read()
    m = re.search(r"^##\s+" + re.escape(head) + r"(?![0-9.])(.*)$", text, re.M)
    if not m:
        rep.add("FAIL", "docs", f"{os.path.relpath(changelog, repo)}: no '## {head}' entry")
    else:
        body = text[m.end():]
        nxt = re.search(r"^##\s", body, re.M)
        body = body[:nxt.start()] if nxt else body
        if not re.search(r"mac ?os|apple silicon|arm64", body, re.I):
            rep.add("FAIL", "docs", f"{os.path.relpath(changelog, repo)}: entry '{head}' does not mention macOS")
        else:
            rep.add("PASS", "docs", f"{os.path.relpath(changelog, repo)}: entry '{head}' mentions macOS")
    cur = re.search(r"Current (?:plugin|app):\s*\*\*v?([^*]+)\*\*", text)
    if cur and cur.group(1) != version:
        rep.add("WARN", "docs", f"{os.path.relpath(changelog, repo)}: header says current {cur.group(1)}, build is {version}")
    for path in (changelog, readme):
        for n, line in enumerate(open(path, encoding="utf-8").read().splitlines(), 1):
            if CJK.search(line):
                rep.add("FAIL", "cjk", f"{os.path.relpath(path, repo)}:{n}: CJK in docs")
    rd = open(readme, encoding="utf-8").read()
    if not re.search(r"mac ?os", rd, re.I):
        rep.add("FAIL", "docs", f"{os.path.relpath(readme, repo)}: no macOS section")
    elif product in ("ofx", "print2383") and not re.search(r"/Library/OFX/Plugins", rd):
        rep.add("FAIL", "docs", f"{os.path.relpath(readme, repo)}: macOS install path /Library/OFX/Plugins not documented")
    elif product in ("ofx", "print2383") and not re.search(r"quarantine|Install\.command", rd):
        rep.add("WARN", "docs", f"{os.path.relpath(readme, repo)}: no quarantine/Install.command note")
    else:
        rep.add("PASS", "docs", f"{os.path.relpath(readme, repo)}: macOS install notes present")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("artifact", nargs="+", help="bundle, .app, folder, .zip or .dmg")
    ap.add_argument("--product", choices=["ofx", "print2383", "app"])
    ap.add_argument("--cjk-only", action="store_true", help="only the CJK scan (for source folders)")
    ap.add_argument("--expect-version", help="version the build must carry, for example 0.90.16")
    ap.add_argument("--repo", help="repository root for the docs/changelog check")
    ap.add_argument("--probe", help="JSON from ofx_version_probe (OfxGetPlugin major/minor)")
    ap.add_argument("--report", help="write a JSON report here")
    ap.add_argument("--mode", choices=["enforce", "report"], default="enforce",
                    help="enforce: exit 1 on FAIL (release builds); report: print the verdict, exit 0")
    args = ap.parse_args()
    if not args.cjk_only and not args.product:
        ap.error("--product is required unless --cjk-only")
    rep = Report()
    versions = []
    work = tempfile.mkdtemp(prefix="nss-w10-")
    try:
        for art in args.artifact:
            base = os.path.basename(art.rstrip("/"))
            if CJK.search(base):
                rep.add("FAIL", "cjk", f"artifact name with CJK: {base!r}")
            m = re.search(r"_v?(\d[0-9A-Za-z.\-+]*?)(?:_(?:mac|macos|darwin|arm64|win64)|\.zip$|\.dmg$)", base, re.I)
            if m and os.path.isfile(art):
                versions.append((f"artifact file name ({base})", m.group(1)))
            root = expand(art, work)
            n = collect(root, rep, versions, args)
            if not args.cjk_only:
                check_layout(root, args.product, rep, art)
            rep.add("INFO", "files", f"{art}: {n} files scanned")
    finally:
        shutil.rmtree(work, ignore_errors=True)
    if args.probe:
        p = json.load(open(args.probe))
        for plug in p.get("plugins", []):
            major, minor = int(plug["major"]), int(plug["minor"])
            if args.product == "ofx":
                v = f"{major}.{minor // 100}.{minor % 100}"
            else:
                v = f"{major}.{minor}"
            versions.append((f"OfxGetPlugin {plug.get('id', '?')} major/minor {major}/{minor}", v))
    if not any(i["check"] == "cjk" and i["level"] == "FAIL" for i in rep.items):
        rep.add("PASS", "cjk", "no CJK characters in names, text, property lists or binary strings")
    if args.cjk_only:
        verdict = "FAIL" if rep.failed else "PASS"
        print(f"RESULT: {verdict}")
        return 1 if rep.failed else 0
    if args.expect_version:
        versions.append(("--expect-version", args.expect_version))
    if not versions:
        rep.add("FAIL", "version", "no version found anywhere in the artifact")
    for where, v in versions:
        if not PLAIN.match(v):
            rep.add("FAIL", "version", f"{where}: '{v}' is not a plain version (digits and dots only)")
    plain = {v for _, v in versions}
    if len(plain) > 1:
        rep.add("FAIL", "version", "versions disagree: " + "; ".join(f"{w} = {v}" for w, v in versions))
    elif versions and all(PLAIN.match(v) for _, v in versions):
        rep.add("PASS", "version", f"all {len(versions)} version sources agree on {versions[0][1]}")
    if args.repo:
        ver = args.expect_version or (versions[0][1] if versions else "")
        check_docs(args.repo, args.product, ver, rep)
    else:
        rep.add("WARN", "docs", "docs/changelog check skipped (no --repo)")
    verdict = "FAIL" if rep.failed else "PASS"
    print(f"RESULT: {verdict}")
    if args.report:
        with open(args.report, "w", encoding="utf-8") as f:
            json.dump({"result": verdict, "mode": args.mode, "versions": versions, "items": rep.items}, f, indent=1)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as f:
            f.write(f"### Mac build checks ({args.product}, {args.mode} mode): **{verdict}**\n\n")
            for i in rep.items:
                if i["level"] in ("FAIL", "WARN", "PASS"):
                    f.write(f"- {i['level']} [{i['check']}] {i['message']}\n")
            f.write("\n")
    if args.mode == "report" and rep.failed:
        print("report mode: FAIL recorded, exit code 0")
        return 0
    return 1 if rep.failed else 0


if __name__ == "__main__":
    sys.exit(main())
