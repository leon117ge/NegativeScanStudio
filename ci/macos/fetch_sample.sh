#!/bin/bash
# Downloads the CC0 sample raw ($SAMPLE_URL) into sample/ and checks its sha256.
set -euo pipefail
mkdir -p sample
f="sample/$SAMPLE"
if [ ! -f "$f" ]; then curl -fsSL --retry 3 -o "$f.part" "$SAMPLE_URL" && mv "$f.part" "$f"; fi
if command -v sha256sum >/dev/null; then got=$(sha256sum "$f" | cut -d' ' -f1); else got=$(shasum -a 256 "$f" | cut -d' ' -f1); fi
[ "$got" = "$SAMPLE_SHA256" ] || { echo "sample sha256 $got, want $SAMPLE_SHA256"; rm -f "$f"; exit 1; }
echo "sample $f ok ($got)"
