# App v0.91 verification

## Executed checks

```sh
python3 tools/check_app_source.py
rg -n '\p{Han}' --glob '*.go' --glob '!ofx/**' --glob '!docs/**' .
LIBRARY_PATH=/workspace/linux-link-libs go test -count=1 \
	./colorsci ./develop ./filmdb ./lightlevel ./basestat ./hdrscan \
	./scanconfig ./rgblight ./camera ./cropper ./rawconv ./scanmeta
LIBRARY_PATH=/workspace/linux-link-libs go vet \
	./colorsci ./develop ./filmdb ./lightlevel ./scanconfig ./rgblight ./scanmeta
```

The source/asset gate passes with zero CJK characters. The separate Go-source scan returns no matches. The gate also rejects injected CJK comments and retired DCTL runtime text in temporary probe files. Those probe files are removed after the check.

All eleven test-bearing shared packages pass. `basestat` has no standalone test files. Existing HDR/trichrome TIFF tests cover metadata round trips, including Unicode roll names and camera-matrix information. The Unicode fixture now uses Go escapes for the same string, not different test data.

The Linux linker uses the existing `/workspace/linux-link-libs/libstdc++.so` alias to the installed C++ runtime. A plain `go test ./...` cannot run the Windows-only root GUI package on Linux. No tests are reported as executed merely because they compile.

The native plugin checks are reproducible with:

```sh
NATIVE_CXX=/workspace/nss-zig-cxx LIBRARY_PATH=/workspace/linux-link-libs \
 bash ofx/test.sh /workspace/nss-ofx-test-0905 /workspace/uploads/0206.tiff
```

The box wrapper invokes `python3 -m ziglang c++`. A normal native C++17 compiler
can be used instead. The suite covers the real loader through mock OFX suites,
CPU math parity, mocked CUDA contracts, overlay events, TIFF/JSON validation,
and legacy unmarked metadata. GPU execution and real Resolve are not claimed.

## Windows cross-build

```sh
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '-H windowsgui -s -w' \
	-o /workspace/NegativeScanStudio_v0.91.exe .
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go test -exec /bin/true ./...
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go test -c -o /workspace/NegativeScanStudio_v0.91.test.exe .
```

The app and all Windows test packages compile. `/bin/true` is a compile-check runner, not a Windows test runtime. The root GUI tests are compiled, not executed.

The executable has a PE/COFF AMD64 header and contains the `NegativeScan Studio v0.91` app-version string.

## Change boundaries

The separately versioned OFX 0.90.5 patch handles the new after-profile marker. `template.dctl` and `template_core.dctl` are also unchanged because OFX regeneration reads them. No app source embeds or exports either template.

Metadata now comes only from accepted after-profile leveling. The checkbox defaults on and persists. Normal scans no longer run the post-merge base measurement. The Light-page calibration check and optional HDR colour-processing measurements remain independent of metadata. RAW conversion, HDR scaling, dust algorithms, crop preservation, and Develop pixel evaluation are unchanged.

`TestScanMetadataUsesLastLevelingNotScene` runs white and trichrome two-frame
HDR paths with dust off and light. Its scene has RGB 0.133028/0.055116/0.038911,
but its accepted reference is 0.49844/0.49639/0.50210. Metadata keeps the latter
exactly, or omits it when the option is off. The test also checks that normal
captures never measure a post-merge Film base. Shared tests cover restart-style
JSON round trips, frozen capture snapshots, and named preset save/load.
Windows-only tests additionally cover the GUI option's persistence and display;
these tests compile but were not executed here.

The OFX native suite exercises the real Load action against both app-produced
TIFF fixtures and an old unmarked fixture. All ten T/D selections and repeated
loads keep the camera-space reference stable. The visible Film base sliders are
`reference * 10^offset`, not lamp drive or an invented 0.5.
Before the patch, applying the camera matrix twice changed the supplied leveling
reference to 0.472882/0.470319/0.506338. OFX 0.90.5 preserves
0.49844/0.49639/0.50210 while keeping pending image correction enabled.

DCTL-only generator/default/dump suites are removed with the retired feature. Existing color-science, Develop, camera, light, image-tool, and metadata tests remain.

## Acceptance still needed

- Run the four-module Windows GUI and check layout, navigation, drag-and-drop, and all command-line page shortcuts.
- Exercise Send base to Develop, Open in Develop, the minimal 500T preview calculator, and the relocated chart-calibration dialog.
- Validate X-T3 and RGB Light Air capture, metering, leveling, Stop, and scan-preset recall on real hardware.
- Confirm a newly captured TIFF loads correctly into the OFX 0.90.5 plugin in Resolve.

Built-in app strings are English. User-entered names, paths, imported data, device errors, and Windows-owned dialogs can still contain other languages. The app does not alter those values or arbitrary imported metadata.
