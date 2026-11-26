# macOS QA in CI

There are two workflows on the `qa/macos-parity` branch. PLAN.md items W8 and
W10 cover them. Neither one signs anything, publishes a release, creates a tag,
or uses a secret. `macos-build-checks` only needs the read-only `GITHUB_TOKEN`
to download artifacts.

## 1. OFX CPU parity, macOS arm64 vs Windows x64 (`.github/workflows/macos-qa.yml`)

**What runs**

`ofx/tests/math_runner.cpp` evaluates `nss::Prepared::pixel`. That is the
per-pixel CPU math the plugin render loop runs (`Prepared::rgba`), built from
the same headers as `plugin.cpp`. The workflow builds it twice:

| Job | Runner | Compiler and flags |
|---|---|---|
| `windows-golden` | windows-2022 | MinGW-w64 `g++ -std=c++17 -O3 -fno-math-errno -fno-trapping-math -static`, the same family and flags as the shipped `ofx/build.sh` |
| `macos-parity` | macos-15 (arm64) | Apple `clang++ -arch arm64` with the same flags as the macOS bundle in `macos-arm64-proof.yml` |

**Inputs**

`ci/qa/parity.py cases` writes 36 deterministic cases of 427 pixels each.

- The pixels are a 24 x 16 synthetic negative: an orange-mask base, an 8-stop
  exposure ramp and seeded noise.
- 43 special pixels are added: 0, 1, out-of-range, the DI log/lin knee, a
  single code, primaries, and a gray ramp.
- Every value is an exact binary fraction, so both platforms parse
  bit-identical float32 inputs. The script generates them with integer tables
  only, without libm.

The cases cover:
- density only, with and without DI input
- all 5 output modes x 4 contrasts
- gray balance
- product default Tone and Tone Color (all switches on) in all 5 modes, plus a
  strong and a shoulder-only setting
- non-default density controls
- out-transform
- a camera matrix at strength 1 and 0.5

**Gates** (`ci/qa/parity.py compare`)

| Check | Rule |
|---|---|
| Inputs | `cases_sha256` must be identical (exact hash) |
| NaN/Inf | must appear at exactly the same positions |
| Max abs error | <= 1e-4 (the PLAN.md section 2 parity bar) |
| Mean abs error | <= 1e-6 |
| Reported, not gated | bit-exact value fraction, identical cases, identical output hash, worst case/pixel/channel, per-case max |

The output is not expected to be bit-exact. libm `expf/logf/powf/exp2f` differs
between the MinGW CRT and Apple libm, and clang contracts `a*b+c` into FMA on
arm64. A diagnostic step rebuilds with `-ffp-contract=off` to show how much
of the difference comes from FMA. That step is not a gate.

The macOS job gates twice:
1. against the Windows golden produced in the same run
2. against the committed golden `ci/qa/golden/ofx_cpu_windows_x64.json`, which
   catches a drift in the Windows output too

The Windows job also compares its fresh output with the committed golden. That
comparison is informational.

**Regenerating the golden after an intended math change**

1. Run the workflow (push to the branch, or Actions > macOS QA parity > Run).
2. Download the artifact `nss-ofx-parity-windows-golden`.
3. Commit its `ofx_cpu_windows_x64.json` as `ci/qa/golden/ofx_cpu_windows_x64.json`
   in the same change as the math change. Say why in the commit message.

The file records the compiler, the flags, the commit and the run link.

**Run it locally on Linux** (compile check plus Wine)

```sh
x86_64-w64-mingw32-g++ -std=c++17 -O3 -fno-math-errno -fno-trapping-math -static ofx/tests/math_runner.cpp -o mr.exe
printf '#!/bin/sh\nexec wine %s "$@"\n' "$PWD/mr.exe" > mr-wine.sh && chmod +x mr-wine.sh
python3 ci/qa/parity.py run ./mr-wine.sh wine.json --label wine
python3 ci/qa/parity.py compare ci/qa/golden/ofx_cpu_windows_x64.json wine.json
```

Wine provides its own CRT math, so its output is not bit-identical to real
Windows. On 2026-11-26 it matched 91.3% of values bit for bit, with a max abs
error of 4.3e-6, which is within the gate. Commit goldens only from the CI
Windows job.

## Measured results

Run [45805981985](https://github.com/leon117ge/NegativeScanStudio/actions/runs/45805981985)
on 2026-11-26 (code from main bd0de46, OFX 0.90.14). It compared 46,116 values:

| Comparison | Max abs | Mean abs | Bit-exact values |
|---|---|---|---|
| macOS arm64 clang vs Windows x64 MinGW g++ 14.2 (gate) | 5.7e-6 | 4.5e-8 | 69.8% |
| macOS arm64 with `-ffp-contract=off` (diagnostic) | 4.3e-6 | 8.6e-9 | 91.1% |

The worst value is in case `density_controls_mode0`, at an output of about 6.1,
where float32 has a relative error of 1e-6. Both rows are well inside the 1e-4
gate.

## 2. Checks on every Mac build (`.github/workflows/macos-build-checks.yml`, `ci/qa/check_mac_build.py`)

`check_mac_build.py ARTIFACT --product ofx|print2383|app [--expect-version X] [--repo .] [--probe probe.json]`
takes a bundle, an `.app`, a folder, a zip (nested zips are opened too) or a
DMG.

**CJK.** It checks:
- file names
- every text file (docs, scripts, JSON, `Install.command`)
- `Info.plist` and binary plists
- the Mach-O string sections (`__cstring`, `__objc_*`, `__oslogstring`)
  strictly
- `__ustring` as UTF-16
- `__const`, `__rodata` and `__data` for runs of 2 or more CJK characters (one
  character could be a numeric table that only looks like text)

The ranges are Han and its extensions, CJK punctuation, full-width forms, kana
and Hangul. Any hit is a FAIL.

**Plain, consistent versions.** Every version source must match
`^\d+(\.\d+){1,3}$`, and all of them must be equal:
- `CFBundleShortVersionString` and `CFBundleVersion`
- the plugin label in the binary (`NegativeScan Studio OFX X.Y.Z`, `NSS Print 2383 X.Y.Z`)
- the app string (`NegativeScan Studio vX.YY`)
- `OfxGetPlugin()` major/minor from `ci/qa/ofx_version_probe.c` (0/9014 is read
  as 0.90.14)
- the version in the artifact file name
- `--expect-version`

**Docs** (with `--repo`):
- The product changelog has a `## <version>` entry (`## v<version>` for the app)
  that mentions macOS.
- The README has a macOS section, the `/Library/OFX/Plugins` path, and a
  quarantine or `Install.command` note.
- Neither file contains CJK.

The checker exits 1 on any FAIL. `ci/qa/test_check_mac_build.py` is its self-test
and must catch `-beta`, a version mismatch, CJK in a text file, in a file name,
and in a Mach-O `__cstring`.

**Calling it from a Mac build workflow** (W1/W2/W3/P5):

```yaml
  checks:
    needs: build            # the job that uploads the Mac artifact
    uses: ./.github/workflows/macos-build-checks.yml
    with:
      artifact-name: NegativeScanStudio-ofx-macos-arm64
      product: ofx
      expect-version: 0.90.16
```
