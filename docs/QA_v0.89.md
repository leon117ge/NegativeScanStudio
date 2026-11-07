# v0.89 verification

Scope: the 500T 3200K illuminant model and DCTL-only offsets.
The later grey-reference and crossover modes are not part of this release.

## Executed on Linux

```sh
LIBRARY_PATH=/workspace/linux-link-libs go test -count=1 \
	./colorsci ./develop ./filmdb ./lightlevel ./basestat ./hdrscan \
	./scanconfig ./rgblight ./camera ./cropper ./rawconv
LIBRARY_PATH=/workspace/linux-link-libs go vet \
	./colorsci ./develop ./filmdb ./lightlevel ./scanconfig ./rgblight
```

All tested packages passed. `basestat` has no test files.
The box has the C++ runtime but lacks its development linker symlink.
For these runs, `/workspace/linux-link-libs/libstdc++.so` points to the
existing `/usr/lib/x86_64-linux-gnu/libstdc++.so.6`. No application source
or system library changed to make the tests pass. A normal Linux build
can use its distro's C++ development package instead.

New tests cover:

- Zero correction at 3200K, green invariance, daylight correction signs,
  monotonic blue correction, Planck normalisation, and invalid K values.
- Emission of DCTL defaults and rejection of missing template parameters.
- Density-domain neutral recovery with synthetic channel offsets.
- Exact legacy pixel output when `cast_on=0`, and unchanged raw transmittance output.
- Film database JSON round trip and preservation of colour-cast parameters on base updates.

`go run ./cmd/illuminantcalc -kelvin 5500` regenerates
`docs/data/5219_5500K_result.json`. These are deterministic model outputs,
not hardware measurements. Their numerical precision is not their physical accuracy.

## Windows cross compile

```sh
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go test -c -o /workspace/NegativeScanStudio_v0.89.test.exe .
CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CXX=x86_64-w64-mingw32-g++ \
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '-H windowsgui -s -w' \
	-o /workspace/NegativeScanStudio_v0.89.exe .
```

Both commands passed. Root GUI tests were compiled, not executed.

## Not verified here

The Windows GUI layout and interactions, Resolve GPU DCTL compilation,
and real X-T3 / RGB Light Air operation have not been exercised.
No same-roll neutral chart is available to validate physical correction accuracy.
The spectral transcription and ECN-2 slopes are approximate. Daylight uses
Planck, not CIE D55. Scanner crosstalk is not calibrated in this model.

The colour-cast controller has no camera/light commands. Existing leveling,
Stop, trichrome, and camera-profile control paths remain unchanged.
