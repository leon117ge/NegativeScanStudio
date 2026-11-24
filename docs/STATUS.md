# Status — 2026-11-24 archive snapshot

This page records where the project stood when the work was normalised and snapshotted. It is a living document: update it when a later version is tested or a backlog item is closed.

| Product | Version | Git tag | Binary |
|---|---|---|---|
| Scanning app | **v1.13** | `v1.13` | `NegativeScanStudio_v1.13.exe` |
| OFX plugin | **0.90.14** | `ofx-v0.90.14` | `NegativeScanStudio_OFX_v0.90.14_Win64.zip` |

Repository: `https://github.com/leon117ge/NegativeScanStudio` (default branch `main`). The repo is a normal, writable project — it was **not** placed in GitHub’s read-only archived state.

Box file archive: `/workspace/nss-archive/` ([INDEX.md](/workspace/nss-archive/INDEX.md) on the box). Snapshot tag: `archive-2026-11-24`.

SHA-256:

- `NegativeScanStudio_v1.13.exe` — `89b34931b15780f786032ca92b1c5b0f9d3b7f4b45fc79d10b8ec263382443b4`
- `NegativeScanStudio_OFX_v0.90.14_Win64.zip` — `63e0a2ce8c3ef5393be869493ac7609158af82815dce05e18ced1c5becca7889`

## What has been tested on hardware

| Area | Status |
|---|---|
| App v1.08 on a real Hikrobot MV-CS240-90UM + CuteNewDesign RGB Light Air | Yes. Live view, Focus Check (as of that release), shared-exposure leveling, Mono Trichrome capture, centre mark. |
| App v1.09–v1.13 on that same body | Written and unit-tested on the box. **v1.13 has not been formally re-signed off on the camera** after the LiveAE and UI work in v1.12–v1.13. Treat those builds as ready for the user to try, not as hardware-certified. |
| X-T3 + RGB Light Air capture / leveling | Implemented and used in earlier 0.9x / 1.0x work (including the 2026-10-31 500T roll). A full four-module pass of the current v1.13 UI on the X-T3 is still listed as validation work. |
| OFX 0.90.6 and earlier | Host tests, CPU math, PoCL OpenCL, mocked CUDA. |
| OFX 0.90.7–0.90.14 | Same class of box tests (`./ofx/test.sh`, Tone tests, PoCL when enabled, offline PTX). **Real Windows Resolve and a physical GPU have not accepted 0.90.12–0.90.14.** The user was looking at 0.90.14 highlight cream in Resolve around 2026-11-24; that feedback is informal and ongoing. |

## Known limits

- OFX 0.90.14 cream and desaturation were tuned and measured through the repo 2383 LUT on frames 0280 / 0206 / F2 / F5 (gray-world balance). 0280 and 0206 are **not** certified 500T; F2 / F5 are from the confirmed 2026-10-31 500T roll. See `/workspace/nss-archive/INDEX.md`.
- The 2383 LUT itself renders neutral blacks cyan and neutral highlights slightly yellow. Shadow desaturation cannot remove the LUT’s cyan floor. An optional shadow tint is an open idea, not implemented.
- On a gray ramp, stock gammas differ by channel; with the shoulder keeping R:G:B, the weakest channel can dip by up to about 0.15 stops (`Soft cinematic`) in very dense highlights. Luminance stays monotonic.
- Positive-chart camera matrices (X-T3 and Monochrome) are not a universal negative dye-crosstalk correction.
- 500T Base (+1.07 / 0 / +0.67 stops) is user eye-tuned, not a Kodak constant. Other stocks have gamma presets but no measured Base loader.
- App: BlackLevel 32, the +5 EV frame’s noise/ghosting, and real capture time vs the 3.6 s estimate still need a check on the Hikrobot body. Diffraction at f/4 on the EX50 is a noted sharpness limit; f/2.8 is untried.
- 0206.tiff has no `nss.scan/1` metadata; Load correctly reports an old scan.

## Open ideas / backlog

App:

- Hardware acceptance of v1.13 Focus Check LiveAE, Mono Stop, and the non-blocking UI on the real Hikrobot and, separately, on the X-T3 path.
- Confirm +5 EV behaviour, BlackLevel at long exposures, and capture timing on film.
- Provenance coverage and missing-value diagnostics that never invent lamp or base values.

Plugin:

- Formal Resolve acceptance: discovery, presets, metadata Load, Open FX Overlay pick, CUDA/OpenCL negotiation, and the 0.90.14 Soft cinematic look on real 500T night and daylight footage through 2383.
- Decide whether a softer density cap is needed for very dense practicals.
- Optional shadow tint if the cyan 2383 blacks remain a problem after that test.
- Measured Base loaders for 250D, UltraMax 400, Portra 400, Gold 200, Ektar 100 — only after measurement.

## Uncommitted and stashed work (kept off `main`)

| Item | Where it lives | What it is |
|---|---|---|
| `ofx/src/metadata.hpp` WIP | Branch `wip/metadata-matrix`, and `/workspace/nss-archive/wip/metadata-matrix.patch` | Optional `mode` / `camera_profile` / `software` fields; null-tolerant `light_rgb` and `camera_matrix`; default `camera_strength` 1 when omitted. Not reviewed or tested as a release. **Do not merge to `main` until tests and a changelog exist.** |
| `stash@{0}`: `wip v0.90 400K presets (stopped by user)` | Left in the git stash. Patch export: `/workspace/nss-archive/wip/stash-wip-v0.90-400K-presets.patch` | Stopped 400K preset experiment. Do not apply or drop the stash unless the owner asks. |

The working tree of `main` / `feature/mono-center-mark` is kept clean of both.
