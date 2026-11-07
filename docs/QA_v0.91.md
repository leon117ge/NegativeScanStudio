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

All ten test-bearing shared packages pass. `basestat` and `scanmeta` have no standalone test files. Existing HDR/trichrome TIFF tests cover metadata round trips, including Unicode roll names and camera-matrix information. The Unicode fixture now uses Go escapes for the same string, not different test data.

The Linux linker uses the existing `/workspace/linux-link-libs/libstdc++.so` alias to the installed C++ runtime. A plain `go test ./...` cannot run the Windows-only root GUI package on Linux. No tests are reported as executed merely because they compile.

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

The OFX tree and version are unchanged. `template.dctl` and `template_core.dctl` are also unchanged because OFX regeneration reads them. No app source embeds or exports either template.

Metadata writers, RAW/TIFF conversion, crop metadata preservation, and Develop pixel evaluation code are unchanged. Only the app software-version value advances. The `scanmetadata.go` edit is a comment correction.

DCTL-only generator/default/dump suites are removed with the retired feature. Existing color-science, Develop, camera, light, image-tool, and metadata tests remain.

## Acceptance still needed

- Run the four-module Windows GUI and check layout, navigation, drag-and-drop, and all command-line page shortcuts.
- Exercise Send base to Develop, Open in Develop, the minimal 500T preview calculator, and the relocated chart-calibration dialog.
- Validate X-T3 and RGB Light Air capture, metering, leveling, Stop, and scan-preset recall on real hardware.
- Confirm a newly captured TIFF loads correctly into the existing OFX plugin in Resolve.

Built-in app strings are English. User-entered names, paths, imported data, device errors, and Windows-owned dialogs can still contain other languages. The app does not alter those values or the metadata format.
