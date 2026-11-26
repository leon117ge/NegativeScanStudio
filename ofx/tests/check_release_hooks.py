#!/usr/bin/env python3
"""Fail if a release binary, object, or unguarded source contains Metal test-hook strings.
Those strings are compiled only into test builds (-DNSS_TEST_HOOKS).

Usage:
  python3 check_release_hooks.py <binary-or-object> [<file>...]
  python3 check_release_hooks.py --source ofx/src/metal_render.mm
"""
import pathlib,sys
HOOKS=(
 b"Metal encode failed (forced by test)",
 b"this is not Metal source",
 b"nss_render_metal_missing",
)

def check_binary(path):
 data=path.read_bytes()
 found=[h.decode() for h in HOOKS if h in data]
 if found:
  print(f"FAIL: {path} contains test-hook string(s): {', '.join(found)}",file=sys.stderr)
  return False
 print(f"PASS: {path}: no Metal test-hook strings")
 return True

def check_source(path):
 text=path.read_text(encoding="utf-8")
 depth=0
 bad=[]
 for number,line in enumerate(text.splitlines(),1):
  stripped=line.strip()
  if stripped.startswith("#ifdef NSS_TEST_HOOKS") or stripped.startswith("#if defined(NSS_TEST_HOOKS)"):
   depth+=1
   continue
  if stripped.startswith("#else") and depth:
   depth=0
   # stay out until this #endif; track with a skip flag
   # treat #else of NSS_TEST_HOOKS as leaving the hook region
   continue
  if stripped.startswith("#endif") and depth:
   depth-=1
   continue
  if depth: continue
  for hook in HOOKS:
   if hook.decode() in line:
    bad.append(f"{path}:{number}: hook string outside NSS_TEST_HOOKS: {hook.decode()}")
 if bad:
  print("\n".join(bad),file=sys.stderr);return False
 print(f"PASS: {path}: hook strings only inside #ifdef NSS_TEST_HOOKS")
 return True

args=sys.argv[1:]
if not args or args[0] in ("-h","--help"):
 print(__doc__.strip(),file=sys.stderr);sys.exit(2)
ok=True
if args[0]=="--source":
 if len(args)<2: print(__doc__.strip(),file=sys.stderr);sys.exit(2)
 for name in args[1:]:
  ok=check_source(pathlib.Path(name)) and ok
else:
 for name in args:
  ok=check_binary(pathlib.Path(name)) and ok
sys.exit(0 if ok else 1)
