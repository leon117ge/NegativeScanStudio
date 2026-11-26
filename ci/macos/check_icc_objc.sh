#!/usr/bin/env bash
# Box-side compile check of the W6 ImageCaptureCore layer without a Mac:
#  0. self-test: the stub headers must reject what the real macOS SDK rejects (an iOS-only
#     ImageCaptureCore call, a missing required delegate method), or the check is worthless;
#  1. camera/icc_darwin.m as Objective-C with ARC, -Wall -Wextra -Werror, against the stub
#     headers in ci/macos/icc_stub (declarations and availability copied from the macOS SDK,
#     never shipped);
#  2. the darwin/arm64 cgo build of ./camera (Go <-> C types, the .m through cgo) and go vet of
#     ./camera and ./cmd/mactether. Nothing is linked (no Apple frameworks on Linux).
# Needs zig (python3 -m ziglang) and Go. On a Mac, CI compiles and links against the real SDK.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d)"
# zig cc leaves "-.o" in the working directory when cgo probes it with "-o -"; remove only ours.
[ -e "$ROOT/-.o" ] && HAD_DASH_O=1 || HAD_DASH_O=0
trap 'rm -rf "$TMP"; [ "$HAD_DASH_O" = 1 ] || rm -f -- "$ROOT/-.o"' EXIT
printf '#!/bin/sh\nexec python3 -m ziglang cc -target aarch64-macos.13.0 "$@"\n' > "$TMP/cc"
chmod +x "$TMP/cc"
STUB="$ROOT/ci/macos/icc_stub"
OBJC=(-x objective-c -fobjc-arc -Wall -Wextra -Wno-unused-parameter -Wunguarded-availability -Werror -I"$STUB")
probe() { # $1 = name, $2 = Objective-C body that the real SDK refuses on macOS
	printf '#import <ImageCaptureCore/ImageCaptureCore.h>\n%s\n' "$2" > "$TMP/$1.m"
	if "$TMP/cc" "${OBJC[@]}" -c "$TMP/$1.m" -o "$TMP/$1.o" 2> "$TMP/$1.err"; then
		echo "FAIL: stub self-test '$1' compiled, but the macOS SDK rejects it"; exit 1
	fi
	echo "ok: stub self-test '$1' rejected: $(grep -m1 -o 'error: .*' "$TMP/$1.err")"
}
probe ios_only_auth 'void f(ICDeviceBrowser *b) { [b requestControlAuthorizationWithCompletion:^(ICAuthorizationStatus s) {}]; }'
probe ios_only_status 'BOOL f(ICDeviceBrowser *b) { return [b.contentsAuthorizationStatus isEqualToString:ICAuthorizationStatusAuthorized]; }'
probe missing_session_delegate '@interface D : NSObject <ICCameraDeviceDelegate>
@end
@implementation D
- (void)didRemoveDevice:(ICDevice *)d {}
- (void)cameraDevice:(ICCameraDevice *)c didAddItems:(NSArray<ICCameraItem *> *)i {}
- (void)cameraDevice:(ICCameraDevice *)c didRemoveItems:(NSArray<ICCameraItem *> *)i {}
- (void)cameraDevice:(ICCameraDevice *)c didReceiveThumbnail:(CGImageRef)t forItem:(ICCameraItem *)i error:(NSError *)e {}
- (void)cameraDevice:(ICCameraDevice *)c didReceiveMetadata:(NSDictionary *)m forItem:(ICCameraItem *)i error:(NSError *)e {}
- (void)cameraDevice:(ICCameraDevice *)c didRenameItems:(NSArray<ICCameraItem *> *)i {}
- (void)cameraDeviceDidChangeCapability:(ICCameraDevice *)c {}
- (void)cameraDevice:(ICCameraDevice *)c didReceivePTPEvent:(NSData *)d {}
- (void)deviceDidBecomeReadyWithCompleteContentCatalog:(ICCameraDevice *)d {}
- (void)cameraDeviceDidRemoveAccessRestriction:(ICDevice *)d {}
- (void)cameraDeviceDidEnableAccessRestriction:(ICDevice *)d {}
@end'
echo "PASS: stub headers reject iOS-only ImageCaptureCore API and missing required delegate methods"
"$TMP/cc" "${OBJC[@]}" -c camera/icc_darwin.m -o "$TMP/icc_darwin.o"
echo "PASS: camera/icc_darwin.m compiles (Objective-C, ARC, -Wall -Wextra -Werror) against the stub SDK headers"
export CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 CC="$TMP/cc" CXX="$TMP/cc" CGO_CFLAGS="-I$STUB -O2"
go build -a ./camera
go vet ./camera ./cmd/mactether
echo "PASS: darwin/arm64 cgo build of ./camera and go vet of ./camera ./cmd/mactether (no link)"
