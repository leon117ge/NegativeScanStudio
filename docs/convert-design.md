# Convert module design (v0.93)

## Reference

- Project: [andreavolpato/spektrafilm](https://github.com/andreavolpato/spektrafilm) — spectral film / print / scan simulation.
- Code license: **GPLv3**. Must not be copied into this MIT repo.
- Profiles / LUTs: **CC BY-SA 4.0** (`SPEKTRAFILM_LICENSE.txt`). Share-alike; **not** copied or ported into this MIT tree.
- Algorithm inspiration only (pipeline shape). Numeric stock data come from **public Kodak datasheets** already digitised in this repo, not from spektrafilm JSON.

## Goal

Replace the last app module **Develop** (negative → positive grading chain) with **Convert**: turn a **digital camera RAW** (Fujifilm X-T3 RAF) into a **simulated colour-negative** 16-bit TIFF.

## Chosen algorithm (justification)

Spektrafilm’s full chain is: RAW → linear RGB → **spectral upsampling** → film log-sensitivities → H&D curves + DIR/masking couplers → dye densities → enlarger → print paper → scan. That is the most complete physical model, but:

1. Its code is GPLv3 and its profile numbers are CC BY-SA — unusable here.
2. Spectral upsampling (e.g. Hanatos 2025) plus 81-band integration is heavy for a first shippable module under a tight usage budget.
3. The user asked for RAW → **film negative**, not the print-and-scan stage.

**Selected model for v0.93 (physically ordered, MIT-safe, budget-fit):**

1. **Decode** with our existing LibRaw path (`rawconv`): linear 16-bit, X-Trans demosaic, `use_camera_matrix = 0`, no LibRaw camera WB/matrix. Same as HDR / Batch RAW.
2. **Camera calibration** with our Chart CamCal matrix via `colorsci.ApplyCam` and `activeCam()` (`camprofile.go` / `colorsci/camcal.go`) — the same matrix leveling uses after “apply camera profile”. Not a generic dcraw/rawpy matrix.
3. **Exposure** (EV stops) on that calibrated linear RGB.
4. **Layer log-exposure** from mid-grey-referenced linear channels: \(H_c = E \cdot RGB_c\), \(x_c = \log_{10}(H_c / 0.18)\).
5. **Characteristic curves** as smooth toe/shoulder sigmoids whose **mid slope** matches the per-stock Status-M / datasheet gamma we already extracted into OFX `still_gammas.inc` / Vision3 presets (Kodak PDF polylines). Dmin provides the orange-mask base density.
6. **Dye mixing**: map layer densities through a 3×3 built from our stock **spectral dye density** curves (`colorsci/stocks.go`, Kodak H-1-5219 / E-4046 / E-4050 / E-7023 / E-7022) integrated against a flat illuminant and `XT3Approx` scanner sensitivities — same spectral machinery as the crosstalk model. This approximates unwanted absorption without copying spektrafilm coupler tables.
7. **Negative output**: transmittance \(T = 10^{-D}\), written as 16-bit linear TIFF (`_NEG.tiff`).

**Explicitly deferred (later versions):** spectral RGB→spectrum upsampling, DIR couplers, grain, halation, print paper / enlarger / scan, Fuji datasheet stocks we have not digitised.

## Stocks (v0.93)

| UI name | Gamma source | Dye spectra source |
|---------|--------------|--------------------|
| Vision3 500T 5219 | OFX Vision3 Status M | Kodak H-1-5219 p.4 |
| Vision3 250D 5207 | OFX 250D preset | H-1-5219 shapes (proxy) |
| UltraMax 400 | `still_gammas.inc` row | Kodak E-7023 (decomposed) |
| Portra 400 | `still_gammas.inc` | Kodak E-4050 (decomposed) |
| Gold 200 | `still_gammas.inc` | Kodak E-7022 (decomposed) |
| Ektar 100 | `still_gammas.inc` | Kodak E-4046 (decomposed) |

## UI

English only. Stock dropdown, exposure EV, optional “apply camera profile”, open RAF/TIFF, preview, export `_NEG.tiff`. Module title **Convert**.

## Calibration file / function used

- Matrix: user Chart profile in `%APPDATA%/NegativeScanStudio/camera_profiles.json` via `activeCam()` → `CamProfile.M` / strength.
- Apply: `colorsci.ApplyCam` (`colorsci/camcal.go`).
- RAW: `rawconv.decode` / `rawconv.Convert` (`rawconv/libraw.go`, `use_camera_matrix = 0`).

## v0.94 scan-matched negative

- Output FS: base = 0.5/0.5/0.5; image = 0.5 * 10^(-(D-Dmin)). No orange cast.
- Rebate border (BorderPct, SoftBorder).
- Metadata: nss.scan/1 film_base after_profile; file name *_RGB.tiff via rawconv.WriteTIFF16Description.
- Round-trip: Density In base 0.5 + Positive Out stock gamma (develop package = dctl_math.hpp).

## v0.95

- Full Status M CSV + PCHIP for still stocks; Vision3 logistic fallback (see convert/data/SOURCES.md).
- Naming OutputPath; grain ISO 100/400 in density domain.


## v1.02 film colour character

v0.93 designed a dye-mixing 3x3 (step 6) but Convert never applied it.
The shipped path was: camera RGB (optional CamCal) -> independent per-channel
H&D -> T = 0.5 * 10^(-D). OFX Positive-Out then inverts those 1D curves, so
the inverted positive was approximately the original digital image. That is
why stocks looked alike, why the result looked digital, and why it read as
a faded/greyish print (scene-linear, no interimage saturation, plus a blue
cast from CSV R/B vs G density mismatch against OFX's simpler gamma model).

**What was wrong in principle vs. in implementation**

- Principle: treating camera R,G,B as film-layer exposures and applying only
  independent 1D characteristic curves cannot produce a film look that
  survives a matching OFX invert. Spectral sensitivity, DIR/interimage and
  chromatic dye mixing are the parts OFX does not undo.
- Principle: Status M characteristic curves already include dye mix *for
  neutrals*. Remixing neutrals again would double-count; chromatic colours
  still need the off-diagonal mix (row-normalised forward Jacobian).
- Principle: base 0.5 is correct (leveled scan). It does not by itself
  flatten contrast — Density In divides by the same 0.5. A simple invert
  that ignored the base *would* look washed out; OFX with base 0.5 does not.
- Implementation: the designed dye-mix step was never coded.
- Implementation: still-film LogHMid is placed on green D-Dmin = 0.75, so a
  grey card's R and B densities differ; OFX's gamma/fog invert does not put
  them back together, leaving a blue/magenta cast (faded look).
- Implementation: Vision3 logistic used Dmax = 1.5, shouldering earlier than
  typical datasheet ranges (~1.6-2.2), which flattened inverted highlights.

**What v1.02 does (still no spektrafilm code)**

1. Camera-linear RGB -> film-layer exposures via a row-normalised overlap
   matrix (XT3Approx vs typical colour-negative sensitivity peaks 650/550/430 nm).
2. Modest system contrast about mid-grey (survives OFX linearisation).
3. Per-channel layer-speed offsets so a grey card inverts neutral.
4. H&D as before (full Status M CSV or logistic; Vision3 logistic Dmax 2.1).
5. DIR/interimage + a warm term (character parameters, not datasheet numbers).
6. Row-normalised per-stock dye Jacobian from `colorsci` dye spectra.
7. Output remains a leveled negative, base 0.5, grain unchanged.

## v1.02-preview2 as-shot white balance

LibRaw decode used by Convert is scan-oriented: `use_camera_wb=0`,
`user_mul=1,1,1,1`, no camera matrix. That is correct for film scans
(the enlarger and Light leveling set the balance). For a scene
photograph it leaves tungsten interiors raw-orange (R much greater
than B). The film model records that orange; OFX inversion then
looks monochrome yellow-brown / sepia.

v1.02-preview2 multiplies camera-RGB by the file's as-shot `cam_mul`
(green-normalized) before the film model. The Convert checkbox
"Apply camera white balance (as-shot)" is on by default. The Chart
CamCal checkbox now defaults off: that matrix is for leveling a scan
camera, not for ordinary photographs.
