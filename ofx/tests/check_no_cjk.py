#!/usr/bin/env python3
"""Fail if Chinese, Japanese or Korean text appears in the given files or folders.

Text files are checked character by character. Binaries are checked string by string:
a NUL-terminated run of valid, printable UTF-8 that contains two CJK characters in a
row is reported (C/C++ string literals are stored that way in Mach-O and PE files;
the NUL bounds and the two-character rule keep machine code from matching by chance).
Usage: check_no_cjk.py <file or folder> [...]
"""
import pathlib, re, sys

CJK = re.compile('[\u2e80-\u2fdf\u3000-\u303f\u3040-\u30ff\u3100-\u31ff\u3200-\u4dbf'
                 '\u4e00-\u9fff\uac00-\ud7af\uf900-\ufaff\ufe30-\ufe4f\uff00-\uffef]')
CJK2 = re.compile(f'{CJK.pattern}{{2}}')
UTF8_RUN = re.compile(rb'(?<=\x00)(?:[\x20-\x7e\t\n\r]|[\xc2-\xdf][\x80-\xbf]|[\xe0-\xef][\x80-\xbf]{2}|[\xf0-\xf4][\x80-\xbf]{3}){2,}(?=\x00)')

def check(path):
    data = path.read_bytes()
    try:
        text = data.decode('utf-8')
        if '\x00' not in text:
            return [f'{path}:{n}: {line.strip()[:80]}' for n, line in enumerate(text.splitlines(), 1) if CJK.search(line)]
    except UnicodeDecodeError:
        pass
    hits = []
    for m in UTF8_RUN.finditer(data):
        s = m.group().decode('utf-8', 'replace')
        if CJK2.search(s):
            hits.append(f'{path}@{m.start():#x}: {s.strip()[:80]!r}')
    return hits

def main():
    files = []
    for arg in sys.argv[1:]:
        p = pathlib.Path(arg)
        files += sorted(x for x in p.rglob('*') if x.is_file()) if p.is_dir() else [p]
    hits = [h for f in files for h in check(f)]
    for h in hits:
        print('CJK:', h)
    print(f"{'FAIL' if hits else 'PASS'}: {len(files)} files checked, {len(hits)} CJK strings")
    return 1 if hits else 0

if __name__ == '__main__':
    sys.exit(main())
