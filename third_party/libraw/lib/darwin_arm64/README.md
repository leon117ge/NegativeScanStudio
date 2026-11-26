# LibRaw for darwin/arm64

`libraw.a` and `libz.a` for Apple Silicon are not committed. They are built
from pinned sources (LibRaw 0.22.1, zlib 1.3.1, sha256-checked) by
`ci/macos/build_libraw.sh`, with the same flags as the vendored
windows_amd64 and linux_amd64 libraries. On a Mac, from the repo root:

    ci/macos/build_libraw.sh
    go test ./rawconv ./hdrscan

The CI workflow `.github/workflows/macos-arm64-core.yml` builds them and
uploads them as the `libraw-darwin-arm64` artifact (with BUILDINFO.txt).
