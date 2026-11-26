# ImageCaptureCore / Foundation stub headers (box check only)

Minimal declarations of the Apple APIs that `camera/icc_darwin.m` uses, with the
signatures of the macOS 13+ SDK. They exist only so `ci/macos/check_icc_objc.sh`
can compile the Objective-C layer on Linux (zig clang, `-fobjc-arc`) and catch
syntax, type and ARC errors before macOS CI runs. They are never shipped, never
linked, and not used on a Mac (the real SDK headers are used there).
