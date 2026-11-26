#!/bin/bash
# Builds the static LibRaw + zlib for darwin/arm64 into
# third_party/libraw/lib/darwin_arm64 (libraw.a, libz.a, BUILDINFO.txt).
# Same LibRaw version, sources and compile flags as the vendored
# windows_amd64 / linux_amd64 libraries (LibRaw 0.22.1, zlib 1.3.1,
# -O3 -DUSE_ZLIB -DLIBRAW_NODLL, no JPEG/LCMS/OpenMP), plus
# -ffp-contract=off so clang does not fuse multiply-adds on arm64 (keeps
# float results in line with the x86-64 builds).
#
# usage: ci/macos/build_libraw.sh [workdir]   (run from the repo root on macOS)
set -euo pipefail

LIBRAW_VERSION=0.22.1
LIBRAW_URL=https://github.com/LibRaw/LibRaw/archive/refs/tags/${LIBRAW_VERSION}.tar.gz
LIBRAW_SHA256=e676248284075605aa2697a66eeed7dc258820bd1d4988c724d29edffd726726
ZLIB_VERSION=1.3.1
ZLIB_URL=https://github.com/madler/zlib/releases/download/v${ZLIB_VERSION}/zlib-${ZLIB_VERSION}.tar.gz
ZLIB_SHA256=9a93b2b7dfdac77ceba5a558a580e74667dd6fede4585b91eefb60f03b72df23

ROOT=$(pwd)
WORK=${1:-$ROOT/build/libraw-darwin-arm64}
OUT=$ROOT/third_party/libraw/lib/darwin_arm64
CC=${CC:-clang}
CXX=${CXX:-clang++}
ARCHFLAGS=${ARCHFLAGS-"-arch arm64 -mmacosx-version-min=13.0 -ffp-contract=off"}
LIBCXX=${LIBCXX:--lc++}
JOBS=$(sysctl -n hw.ncpu 2>/dev/null || nproc)

# Runs each line of a file as a shell command, $JOBS at a time; fails if any fails.
# (No xargs -I: BSD xargs limits the replacement to 255 bytes.)
runall() { tr '\n' '\0' < "$1" | xargs -0 -n 1 -P "$JOBS" sh -c; }

mkdir -p "$WORK" "$OUT"
cd "$WORK"

fetch() { # url sha256 file
	if [ ! -f "$3" ]; then curl -fsSL --retry 3 -o "$3.part" "$1" && mv "$3.part" "$3"; fi
	got=$(shasum -a 256 "$3" | cut -d' ' -f1)
	if [ "$got" != "$2" ]; then echo "sha256 mismatch for $3: $got (want $2)"; exit 1; fi
}
fetch "$LIBRAW_URL" "$LIBRAW_SHA256" LibRaw-${LIBRAW_VERSION}.tar.gz
fetch "$ZLIB_URL" "$ZLIB_SHA256" zlib-${ZLIB_VERSION}.tar.gz
rm -rf src && mkdir src && cd src
tar xzf ../LibRaw-${LIBRAW_VERSION}.tar.gz
tar xzf ../zlib-${ZLIB_VERSION}.tar.gz
SRC=$WORK/src
mkdir -p obj zobj

# zlib
cd "$SRC/zlib-${ZLIB_VERSION}"
: > "$SRC/zcmds.txt"
for f in adler32 compress crc32 deflate gzclose gzlib gzread gzwrite infback inffast inflate inftrees trees uncompr zutil; do
	echo "$CC $ARCHFLAGS -O2 -DHAVE_UNISTD_H -DHAVE_STDARG_H -c $f.c -o $SRC/zobj/$f.o" >> "$SRC/zcmds.txt"
done
runall "$SRC/zcmds.txt"
rm -f "$OUT/libz.a"
ar rcs "$OUT/libz.a" "$SRC"/zobj/*.o
ranlib "$OUT/libz.a"

# LibRaw: the LIB_OBJECTS list from Makefile.dist (the library, no samples).
cd "$SRC/LibRaw-${LIBRAW_VERSION}"
objs=$(sed -n '/^LIB_OBJECTS/,/^$/p' Makefile.dist | grep -o 'object/[a-z0-9_]*\.o' | sed 's#object/##;s#\.o##')
: > "$SRC/cmds.txt"
for o in $objs; do
	src=$(find src -name "$o.cpp" | head -1)
	[ -z "$src" ] && { echo "missing source for $o"; exit 1; }
	echo "$CXX $ARCHFLAGS -O3 -I. -I$SRC/zlib-${ZLIB_VERSION} -w -DUSE_ZLIB -DLIBRAW_NODLL -c $src -o $SRC/obj/$o.o" >> "$SRC/cmds.txt"
done
runall "$SRC/cmds.txt"
n=$(ls "$SRC"/obj/*.o | wc -l | tr -d ' ')
want=$(echo $objs | wc -w | tr -d ' ')
[ "$n" = "$want" ] || { echo "built $n of $want objects"; exit 1; }
rm -f "$OUT/libraw.a"
ar rcs "$OUT/libraw.a" "$SRC"/obj/*.o
ranlib "$OUT/libraw.a"

# The headers the Go package compiles against must be this version.
diff -r libraw "$ROOT/third_party/libraw/include/libraw" >/dev/null || { echo "vendored headers differ from LibRaw $LIBRAW_VERSION"; exit 1; }

# Version check: link a tiny program against the new libraries.
cat > "$SRC/ver.c" <<'C'
#include <stdio.h>
#include "libraw/libraw.h"
int main(void) { printf("%s\n", libraw_version()); return 0; }
C
$CXX $ARCHFLAGS -x c "$SRC/ver.c" -x none -I. -DLIBRAW_NODLL -L"$OUT" -lraw -lz $LIBCXX -lm -o "$SRC/ver"
v=$("$SRC/ver")
[ "$v" = "${LIBRAW_VERSION}-Release" ] || { echo "linked LibRaw reports $v"; exit 1; }

{
	echo "LibRaw ${LIBRAW_VERSION} ($v) from $LIBRAW_URL sha256 $LIBRAW_SHA256"
	echo "zlib ${ZLIB_VERSION} from $ZLIB_URL sha256 $ZLIB_SHA256"
	echo "flags: $ARCHFLAGS -O3 -DUSE_ZLIB -DLIBRAW_NODLL (zlib -O2 -DHAVE_UNISTD_H -DHAVE_STDARG_H)"
	echo "compiler: $($CXX --version | head -1)"
	echo "objects: $n"
	(cd "$OUT" && shasum -a 256 libraw.a libz.a)
	lipo -info "$OUT/libraw.a" 2>/dev/null || true
} | tee "$OUT/BUILDINFO.txt"
