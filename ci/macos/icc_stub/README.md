# ImageCaptureCore / Foundation stub headers (box check only)

Minimal declarations of the Apple APIs that `camera/icc_darwin.m` uses. The
ImageCaptureCore declarations, including `API_AVAILABLE`/`API_UNAVAILABLE`
attributes and `@required` delegate methods, are copied from the macOS SDK
headers (MacOSX15.5.sdk and MacOSX26.5.sdk; 13.3 and 14.5 have the same
availability), so iOS-only API and missing required delegate methods fail the
`-Werror` compile here exactly as on a Mac. `check_icc_objc.sh` self-tests that. They exist only so `ci/macos/check_icc_objc.sh`
can compile the Objective-C layer on Linux (zig clang, `-fobjc-arc`) and catch
syntax, type and ARC errors before macOS CI runs. They are never shipped, never
linked, and not used on a Mac (the real SDK headers are used there).
