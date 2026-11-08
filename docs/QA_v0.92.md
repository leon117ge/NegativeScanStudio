# App v0.92 local-test verification

## Scope

This build changes Light-page leveling and its calibration TIFF statistic. The OFX plugin stays at 0.90.6. Plugin sources and the two generator templates are unchanged. No tag, push, or GitHub release is part of this local acceptance build.

## Executed tests

```sh
python3 tools/check_app_source.py
LIBRARY_PATH=/workspace/linux-link-libs go test -json -count=1 \
	./colorsci ./develop ./filmdb ./lightlevel ./basestat ./hdrscan \
	./scanconfig ./rgblight ./camera ./cropper ./rawconv ./scanmeta
LIBRARY_PATH=/workspace/linux-link-libs go test -count=1 \
	levelingrecord.go levelingrecord_test.go
LIBRARY_PATH=/workspace/linux-link-libs go vet \
	./colorsci ./develop ./filmdb ./lightlevel ./basestat ./hdrscan \
	./scanconfig ./rgblight ./camera ./cropper ./rawconv ./scanmeta
```

The shared packages and the standalone CSV/reference tests pass. The source gate reports zero CJK characters in app source and embedded text assets. `go vet` passes.

The new regression tests run without hardware or optional input files. They cover:

- A five-percent bright-pixel patch that made the old green channel alternate between 100 and 50.
- Coarse integer codes that cannot meet the requested tolerance, including the default two percent.
- Nonlinear responses and deterministic frame noise.
- Dead channels, saturation at code 1, and insufficient output at code 100.
- Known matrix coupling and minimax selection among integer neighbours.
- A final measurement that drifts away from a previously acceptable cached value.
- Bad starting codes across a deterministic response sweep.
- Deferred Auto selection after initial clipping, with one frozen rectangle.
- Exact equality between control samples and the real trichrome TIFF path with a nonuniform synthetic image, hot samples, dust-like dark samples, and a non-grid-aligned rectangle.
- Failed CSV fields and preservation of the previous accepted metadata base.

The same first three regression cases fail against the pre-change `05dc3a0` solver. The baseline demonstrates the 100-to-50 bounce, false closest-step success, and an unphotographed final code. The current cases pass. Logs are saved locally as `/workspace/nss-v092-red-current-model.log` and `/workspace/nss-v092-regression-green.log`.

Optional historical tests remain skipped when `CHART_OUT`, `NEUTRAL_TIFF`, `CAMCAL_TIFF`, `CAMCAL_REF`, or `HDR_SYNTH_DIR` are unset. Historical trichrome camera tests skip because `testdata/negsamples2/_DSF9229.RAF` is absent. `basestat` has no standalone test files. These skipped cases are not reported as executed tests.

## Windows compilation

```sh
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go test -exec /bin/true ./...
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go test -c \
	-o /workspace/NegativeScanStudio_v0.92.test.exe .
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go build -trimpath \
	-ldflags '-H windowsgui -s -w' \
	-o /workspace/NegativeScanStudio_v0.92.exe .
```

All Windows packages and the root GUI tests compile. `/bin/true` is a compile-check runner, not a Windows runtime. The executable uses the v0.91 build flags. Its PE header, embedded app version, Go build information, and SHA-256 are checked after the build.

The new Windows-only configuration test checks the 10-round default, migration of the old four-round default, preservation of another chosen limit, and a newly saved four-round limit. That test is compiled but not executed on this Linux box.

## Hardware acceptance

Real Windows GUI interaction, Rectangle selection, Stop, X-T3 capture, RGB Light Air output and settling, thermal drift, and elapsed leveling time remain untested. Control now uses actual linear demosaicing rather than a RAW-plane estimate. It can take longer per frame. Default settling remains 300 ms, and exposure does not change automatically.

A hardware test should compare repeated readings of the same rectangle and check the final TIFF base, failure diagnostics, saved metadata reference, and normal capture after leveling. A failed new run preserves the previous accepted base, but the reference still needs matching lamp codes and unchanged film, profile, exposure, and optics. Existing scan TIFFs are not rewritten.
