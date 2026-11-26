#!/usr/bin/env bash
# Build the CI stand-in for Hikrobot's MvCameraControl (not the real SDK).
# Usage: bash ci/macos/build_mvs_stub.sh [out-dir]
# Prints the library path; export it as NSS_MVS_LIB for hikcam tests.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${1:-$here/../../out/mvs-stub}"
mkdir -p "$out"
cc="${CC:-cc}"
case "$(uname -s)" in
  Darwin)
    lib="$out/libMvCameraControlStub.dylib"
    "$cc" -std=c99 -O2 -Wall -Wextra -Werror -fvisibility=hidden -dynamiclib \
      -mmacosx-version-min=13.0 -install_name @rpath/libMvCameraControlStub.dylib \
      -o "$lib" "$here/mvs_stub.c" ;;
  *)
    lib="$out/libMvCameraControlStub.so"
    "$cc" -std=c99 -O2 -Wall -Wextra -Werror -fvisibility=hidden -fPIC -shared \
      -o "$lib" "$here/mvs_stub.c" ;;
esac
echo "$lib"
