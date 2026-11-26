#!/usr/bin/env bash
# Writes the MSL copy of the shared math header. Only change: '(float *' and ',float *'
# parameters become 'thread float *' (MSL needs an explicit address space).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
sed -E 's/([(,])float \*/\1thread float */g' "$ROOT/ofx/src/render_math.h" > "${1:-$ROOT/build/metal/render_math_msl.h}"
