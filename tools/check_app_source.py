#!/usr/bin/env python3
"""Check English app sources/embedded assets and prevent retired DCTL exports."""
from pathlib import Path
import glob
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
# Use Git's file list so local build backups and historical scratch files are not
# mistaken for app inputs. Include new, unignored app files before committing.
listed = subprocess.check_output(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=ROOT
).decode().split("\0")
files = {
    ROOT / name for name in listed
    if name and not name.startswith(("docs/", "ofx/"))
    and (ROOT / name).is_file()
    and Path(name).suffix in {".go", ".manifest", ".rc", ".dctl"}
}
for path in list(files):
    if path.suffix != ".go":
        continue
    for pattern in re.findall(r"^//go:embed\s+(\S+)\s*$", path.read_text(), re.M):
        for match in glob.glob(str(path.parent / pattern)):
            asset = Path(match)
            if asset.is_file():
                files.add(asset)

han = re.compile("[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\U00020000-\U000323af]")
tokens = re.compile(r'//[^\n]*|/\*.*?\*/|"(?:\\.|[^"\\])*"|`[^`]*`', re.S)
errors = []
text_count = 0
for path in sorted(files):
    try:
        text = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:  # binary ICC assets are not app UI text
        continue
    text_count += 1
    for number, line in enumerate(text.splitlines(), 1):
        if han.search(line):
            errors.append(f"{path.relative_to(ROOT)}:{number}: CJK character")
    # Historical comments and compatibility fixtures can name DCTL. Runtime
    # app text must not advertise a retired page, exporter, or setting.
    if path.suffix == ".go" and not path.name.endswith("_test.go") and not path.relative_to(ROOT).parts[0] in {"cmd", "third_party"}:
        for token in tokens.finditer(text):
            literal = token.group()
            if literal.startswith(('"', '`')) and re.search(r"dctl", literal, re.I):
                errors.append(f"{path.relative_to(ROOT)}: retired DCTL runtime string: {literal}")

retired = ["FilmScan_MaskCrosstalk.dctl",
           "maskdctl.go", "maskdefaults.go", "maskpage.go", "wbpage.go"]
for name in retired:
    if (ROOT / name).exists():
        errors.append(f"retired app file still exists: {name}")
for path in ROOT.glob("*.go"):
    if path.name.endswith("_test.go"):
        continue
    text = path.read_text()
    if re.search(r"//go:embed[^\n]*\.dctl|\b(?:DCTLPath|AutoSync|sendDCTL|writeDensityDCTL|buildCoreDCTL|writeLevelingDCTL)\b", text):
        errors.append(f"retired export symbol in {path.name}")
if errors:
    print("\n".join(errors), file=sys.stderr)
    sys.exit(1)
print(f"PASS: {text_count} app source/text-asset files, zero CJK characters; no retired DCTL files, export symbols, or runtime text")
