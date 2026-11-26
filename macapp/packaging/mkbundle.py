#!/usr/bin/env python3
"""Assemble NegativeScan Studio.app around a built binary (no Pillow, no exec bits needed).

usage: python3 mkbundle.py <binary> <out-dir>   -> <out-dir>/NegativeScan Studio.app

- Info.plist from Info.plist.in, version = appVersion in ../../hdrpage.go (the single
  version source of the app; "v1.13" -> 1.13). No version is written by hand here.
- AppIcon.icns from the Windows icon ../../ico06/app.ico: its PNG entries are copied
  unchanged into the matching ICNS slots (icp4 16, icp5 32, icp6 64, ic11 32 @2x,
  ic12 64 @2x, ic08 256). macOS scales the 256 px image for larger sizes.
- Resources/appicon.png: the largest PNG entry of the same .ico, read by the app for
  the in-window header icon (the repository holds no copy of it).
"""
import os
import re
import shutil
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))


def app_version():
    src = open(os.path.join(ROOT, "hdrpage.go"), encoding="utf-8").read()
    m = re.search(r'^const appVersion = appName \+ " v([0-9]+(?:\.[0-9]+)*)"', src, re.M)
    if not m:
        sys.exit("appVersion not found in hdrpage.go")
    return m.group(1)


def ico_pngs(path):
    b = open(path, "rb").read()
    _, typ, n = struct.unpack_from("<HHH", b, 0)
    if typ != 1:
        sys.exit("not an .ico: " + path)
    out = {}
    for i in range(n):
        w, h, _, _, _, _, size, off = struct.unpack_from("<BBBBHHII", b, 6 + 16 * i)
        data = b[off:off + size]
        if data[:8] == b"\x89PNG\r\n\x1a\n":
            out[w or 256] = data
    return out


def icns(pngs):
    slots = [("icp4", 16), ("icp5", 32), ("icp6", 64), ("ic11", 32), ("ic12", 64), ("ic08", 256)]
    body = b""
    for tag, size in slots:
        if size in pngs:
            body += tag.encode() + struct.pack(">I", 8 + len(pngs[size])) + pngs[size]
    if not body:
        sys.exit("no PNG icons in the .ico")
    return b"icns" + struct.pack(">I", 8 + len(body)) + body


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    binary, out = sys.argv[1], sys.argv[2]
    ver = app_version()
    app = os.path.join(out, "NegativeScan Studio.app")
    if os.path.exists(app):
        shutil.rmtree(app)
    macos = os.path.join(app, "Contents", "MacOS")
    res = os.path.join(app, "Contents", "Resources")
    os.makedirs(macos)
    os.makedirs(res)
    exe = os.path.join(macos, "NegativeScanStudio")
    shutil.copyfile(binary, exe)
    os.chmod(exe, 0o755)
    plist = open(os.path.join(HERE, "Info.plist.in"), encoding="utf-8").read().replace("@VERSION@", ver)
    open(os.path.join(app, "Contents", "Info.plist"), "w", encoding="utf-8").write(plist)
    open(os.path.join(app, "Contents", "PkgInfo"), "w").write("APPL????")
    pngs = ico_pngs(os.path.join(ROOT, "ico06", "app.ico"))
    open(os.path.join(res, "AppIcon.icns"), "wb").write(icns(pngs))
    open(os.path.join(res, "appicon.png"), "wb").write(pngs[max(pngs)])
    print(app)
    print("version " + ver)


if __name__ == "__main__":
    main()
