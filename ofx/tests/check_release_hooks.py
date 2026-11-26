#!/usr/bin/env python3
"""Fail if a release binary, object, or unguarded source contains Metal test-hook strings.
Those strings are compiled only into test builds (-DNSS_TEST_HOOKS).

Usage:
  python3 check_release_hooks.py <binary-or-object> [<file>...]
  python3 check_release_hooks.py --source ofx/src/metal_render.mm
  python3 check_release_hooks.py --self-test
"""
import pathlib,re,sys
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

DIRECTIVE=re.compile(r"^\s*#\s*(if|ifdef|ifndef|elif|else|endif)\b(.*)$")
HOOK_ON=re.compile(r"^(?:ifdef\s+NSS_TEST_HOOKS|if\s+defined\s*\(?\s*NSS_TEST_HOOKS\s*\)?)\s*(?://.*|/\*.*)?$")
HOOK_OFF=re.compile(r"^(?:ifndef\s+NSS_TEST_HOOKS|if\s+!\s*defined\s*\(?\s*NSS_TEST_HOOKS\s*\)?)\s*(?://.*|/\*.*)?$")

def unguarded(text):
 """Line numbers and hooks of hook strings outside every NSS_TEST_HOOKS-only region.
 A stack holds one entry per open #if/#ifdef/#ifndef: None for an unrelated condition, or
 whether the current branch of an NSS_TEST_HOOKS condition is the hooks-on branch. #else flips
 that branch, #elif leaves it (conservative: treated as not hooks-only), #endif pops."""
 stack=[]
 bad=[]
 for number,line in enumerate(text.splitlines(),1):
  m=DIRECTIVE.match(line)
  if m:
   word,rest=m.group(1),(m.group(1)+" "+m.group(2).strip()).strip()
   if word in ("if","ifdef","ifndef"):
    stack.append(True if HOOK_ON.match(rest) else False if HOOK_OFF.match(rest) else None)
   elif word=="else" and stack:
    if stack[-1] is not None: stack[-1]=not stack[-1]
   elif word=="elif" and stack:
    if stack[-1] is not None: stack[-1]=False
   elif word=="endif" and stack:
    stack.pop()
   continue
  if any(state is True for state in stack): continue
  for hook in HOOKS:
   if hook.decode() in line:
    bad.append((number,hook.decode()))
 return bad

def check_source(path):
 bad=[f"{path}:{n}: hook string outside NSS_TEST_HOOKS: {h}" for n,h in unguarded(path.read_text(encoding="utf-8"))]
 if bad:
  print("\n".join(bad),file=sys.stderr);return False
 print(f"PASS: {path}: hook strings only inside #ifdef NSS_TEST_HOOKS")
 return True

H='"this is not Metal source"'
SELF_TESTS=(
 ("guarded",f"#ifdef NSS_TEST_HOOKS\nx={H};\n#endif\n",[]),
 ("unguarded",f"x={H};\n",[1]),
 ("nested #if inside the hook region",f"#ifdef NSS_TEST_HOOKS\n#if A\ny=1;\n#endif\nx={H};\n#endif\n",[]),
 ("nested #if/#else inside the hook region",f"#if defined(NSS_TEST_HOOKS)\n#ifdef A\n#else\n#endif\nx={H};\n#endif\n",[]),
 ("#else of the hook region is release code",f"#ifdef NSS_TEST_HOOKS\n#else\nx={H};\n#endif\n",[3]),
 ("#ifndef then #else",f"#ifndef NSS_TEST_HOOKS\nx={H};\n#else\ny={H};\n#endif\n",[2]),
 ("hook region inside an unrelated #if",f"#if A\n#ifdef NSS_TEST_HOOKS\nx={H};\n#endif\ny={H};\n#endif\n",[5]),
 ("after the region closes",f"#ifdef NSS_TEST_HOOKS\n#if A\n#endif\n#endif\nx={H};\n",[5]),
 ("#elif leaves the hook branch",f"#ifdef NSS_TEST_HOOKS\n#elif B\nx={H};\n#endif\n",[3]),
 ("spaced directive",f"#  ifdef NSS_TEST_HOOKS // tests\nx={H};\n# endif\n",[]),
 ("other macro is not a guard",f"#ifdef NSS_TEST_HOOKS_X\nx={H};\n#endif\n",[2]),
)

def self_test():
 ok=True
 for name,text,want in SELF_TESTS:
  got=[n for n,_ in unguarded(text)]
  if got!=want:
   print(f"FAIL: self-test {name}: lines {got}, want {want}",file=sys.stderr);ok=False
 if ok: print(f"PASS: check_release_hooks self-test ({len(SELF_TESTS)} nesting cases)")
 return ok

args=sys.argv[1:]
if not args or args[0] in ("-h","--help"):
 print(__doc__.strip(),file=sys.stderr);sys.exit(2)
ok=True
if args[0]=="--self-test":
 ok=self_test()
elif args[0]=="--source":
 if len(args)<2: print(__doc__.strip(),file=sys.stderr);sys.exit(2)
 for name in args[1:]:
  ok=check_source(pathlib.Path(name)) and ok
else:
 for name in args:
  ok=check_binary(pathlib.Path(name)) and ok
sys.exit(0 if ok else 1)
