#!/usr/bin/env python3
"""Check otool -l (or vtool -show-build) output for a minimum macOS of 13.0.
Usage: python3 check_mac_minos.py \u003cotool-l-or-vtool.txt\u003e"""
import pathlib,re,sys
if len(sys.argv)!=2:
 print("usage: check_mac_minos.py \u003cotool-l.txt\u003e",file=sys.stderr);sys.exit(2)
text=pathlib.Path(sys.argv[1]).read_text()
ok=bool(re.search(r"minos 13\.0",text) or re.search(r"^\s*version 13\.0",text,re.M))
bad=bool(re.search(r"minos 12\.|^\s*version 12\.",text,re.M))
if ok and not bad:
 print("PASS: Mach-O minimum macOS 13.0");sys.exit(0)
print(text,file=sys.stderr);print("FAIL: expected Mach-O minos 13.0",file=sys.stderr);sys.exit(1)
