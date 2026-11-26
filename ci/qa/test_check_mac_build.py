#!/usr/bin/env python3
"""Self-test for check_mac_build.py: it must catch CJK and non-plain versions.

Builds small fixture bundles in a temp folder. CJK test text is written with
Python escapes so this file itself stays ASCII. If clang can target macOS
(on a macOS runner), a real Mach-O with a CJK C string is also checked.
"""
import os
import plistlib
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
CHECK = [sys.executable, os.path.join(HERE, "check_mac_build.py")]
HAN = "\u6d4b\u8bd5"  # two Han characters


def bundle(root, short, build, label_version, readme_text="English only.\n"):
    c = os.path.join(root, "NegativeScanStudio.ofx.bundle", "Contents")
    os.makedirs(os.path.join(c, "MacOS"))
    with open(os.path.join(c, "Info.plist"), "wb") as f:
        plistlib.dump({"CFBundleShortVersionString": short, "CFBundleVersion": build,
                       "CFBundleExecutable": "NegativeScanStudio.ofx"}, f)
    with open(os.path.join(c, "MacOS", "NegativeScanStudio.ofx"), "wb") as f:
        # Not a Mach-O: scanned as a plain binary. Carries the label string.
        f.write(b"\0\1\2NegativeScan Studio OFX " + label_version.encode() + b"\0\xff\xfe")
    with open(os.path.join(root, "README.txt"), "w", encoding="utf-8") as f:
        f.write(readme_text)
    return root


def run(path, expect_fail, why, *extra):
    r = subprocess.run(CHECK + [path, "--product", "ofx", *extra], capture_output=True, text=True)
    ok = (r.returncode != 0) == expect_fail
    print(("ok   " if ok else "BAD  ") + why)
    if not ok:
        print(r.stdout, r.stderr)
    return ok


def main():
    tmp = tempfile.mkdtemp()
    results = []
    try:
        results.append(run(bundle(os.path.join(tmp, "good"), "0.90.16", "0.90.16", "0.90.16"), False,
                           "clean bundle with matching plain versions passes"))
        results.append(run(bundle(os.path.join(tmp, "beta"), "0.90.16-beta", "0.90.16", "0.90.16"), True,
                           "suffix -beta in CFBundleShortVersionString fails"))
        results.append(run(bundle(os.path.join(tmp, "mismatch"), "0.90.16", "0.90.16", "0.90.15"), True,
                           "plist 0.90.16 vs binary label 0.90.15 fails"))
        results.append(run(bundle(os.path.join(tmp, "doc"), "0.90.16", "0.90.16", "0.90.16",
                                  "Install " + HAN + "\n"), True, "CJK in a shipped text file fails"))
        results.append(run(bundle(os.path.join(tmp, "expect"), "0.90.16", "0.90.16", "0.90.16"), True,
                           "--expect-version 0.90.17 vs build 0.90.16 fails", "--expect-version", "0.90.17"))
        name = os.path.join(tmp, "name")
        bundle(name, "0.90.16", "0.90.16", "0.90.16")
        open(os.path.join(name, HAN + ".txt"), "w").write("x")
        results.append(run(name, True, "CJK in a file name fails"))
        clang = shutil.which("clang")
        if clang and sys.platform == "darwin":
            mo = os.path.join(tmp, "macho")
            bundle(mo, "0.90.16", "0.90.16", "0.90.16")
            src = os.path.join(tmp, "s.c")
            with open(src, "w", encoding="utf-8") as f:
                f.write('#include <stdio.h>\nint main(void){puts("NegativeScan Studio OFX 0.90.16 '
                        + HAN + '");return 0;}\n')
            exe = os.path.join(mo, "NegativeScanStudio.ofx.bundle", "Contents", "MacOS", "NegativeScanStudio.ofx")
            subprocess.run([clang, "-arch", "arm64", src, "-o", exe], check=True)
            results.append(run(mo, True, "CJK in a Mach-O __cstring fails"))
            with open(src, "w", encoding="utf-8") as f:
                f.write('#include <stdio.h>\nint main(void){puts("NegativeScan Studio OFX 0.90.16");return 0;}\n')
            subprocess.run([clang, "-arch", "arm64", src, "-o", exe], check=True)
            results.append(run(mo, False, "clean Mach-O passes"))
        else:
            print("skip Mach-O fixtures (needs clang on macOS)")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    print(f"{sum(results)}/{len(results)} self-tests passed")
    return 0 if all(results) else 1


if __name__ == "__main__":
    sys.exit(main())
