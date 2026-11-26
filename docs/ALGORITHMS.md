# Algorithms

> Historical reference through v0.90. App v0.91 removes the DCTL page, all DCTL generation/export, and related write controls. The formulas below remain relevant to the built-in Develop preview, but the old page layout and export instructions are retired. See [current app workflow](APP_WORKFLOW.md), [v0.91 changelog](CHANGELOG.md#v091), and [OFX usage](../ofx/README.md).


All descriptions come from source code comments and the implementation.

## 1. Film base statistics and detection (`basestat`, `hdrscan/basefind.go`)
- Value range: linear 16-bit / 65535. In a clean film base region, light leaks (pixels that deviate from clean film base) are rejected by R+G+B, then the median is taken per channel.
- Auto mode: `hdrscan` finds the brightest and most uniform film base region in the frame.
- Manual selection (since v0.87): the user drags a rectangle on the live view (normalized coordinates `lightlevel.Rect`); in Rectangle mode only that box is measured, and leveling is refused when no box is drawn. The normalized box recorded in Auto mode is all zeros.

## 2. Film-base light leveling in v0.92

The current solver uses measured slopes, bounded secant steps, bisection, matrix-aware integer refinement, and a fresh final RGB capture. Control and the calibration TIFF use the same pixels and median. The default limit is 10 coarse rounds. See [the complete leveling reference](LIGHT_LEVELING.md).

## 3. Camera color chart calibration (`colorsci/camcal.go`, `chartcal.go`, v0.84)
- 24-patch transmissive color chart (4×6, row A is the neutral gray scale A1–A6); the reference data is IT8.7/1 CGATS (XYZ D50 or Lab).
- `DetectCamChart` finds and orients the grid in the linear 16-bit image (row A luminance increasing, channel ratios stable → `rampScore`).
- Clipping detection: the fraction of pixels above a threshold decides whether a patch is clipped.
- Fits a **neutral-preserving 3×3 matrix** (a per-row sum constraint keeps gray gray), minimizing the mean **CIEDE2000**; color conversion XYZ D50 → Bradford → D65 linear Rec709.

## 4. Density-space DCTLs
- **FilmScan_Density_In**: linear scan / film base (`base_r/g/b`) → Cineon-style density code. Parameters: film base code `dmin_code`=95, density per code 0.002, full scale 1023, backlight exposure offset, black level subtraction, etc.
- **FilmScan_MaskCrosstalk**: orange film base compensation in density space (`bd_r/g/b`) and a 3×3 dye crosstalk matrix; no change by default.
- **FilmScan_Positive_Out**: per-channel gamma (default R 0.55 / G 0.635 / B 0.62), exposure, balance, gray point, output encoding (Rec709 / DaVinci Intermediate / Cineon / ACEScct / Linear); a Kodak 2383 print LUT can follow it.
- The `develop` package reproduces this chain 1:1 in float32 for the in-app preview.

## 5. HDR merge (`hdrscan/merge*.go`)
- `MergeN`: frame 0 + brighter +2/+3/+4 EV frames, merged on the frame 0 scale.
  1. Inter-frame ratios are measured as per-channel medians on pixels where "the bright frame is not clipped and the dark frame is above the 1 % noise floor", and multiplied in a chain;
  2. On failure, the nominal ratio of exposure time × ISO is used, corrected by the measured/nominal ratio of the +2 frame;
  3. Without exposure information, the +2 frame is assumed to be 4×.
- Blending goes from dark to bright; the blend band is driven by the (dilated) clipping mask of the bright frame; within ~1 EV of the film base, the original frame 0 values are kept.
- Weighted / sensor mosaic method (v0.80): film base protection band ≥0.85·L uses frame 0, <0.70·L uses the merged result; can average per photosite.
- Supports alignment (shift estimation of ±r px on a central green crop).

## 6. Hair / fiber removal (`hdrscan/hair.go`, v0.77)
Inside the user-selected region: L = log2(R+G+B+3) at half resolution → 24-direction second-derivative kernels → robust Z score (MAD) + non-maximum suppression Z≥2 → connected grouping, taking the longest fiber → full-resolution centerline refinement and repair band generation → split into 24 px blocks along the centerline, pick the best of 24 offset candidate blocks by annulus log matching, then blend seamlessly with Shepard membrane interpolation. Pixels outside the band are unchanged; full-frame automatic detection is not offered (it would falsely detect real lines).

## 7. Cropping (`cropper`)
Detects the four borders between the image and the film base/film holder (`Side`/`Edge`), crops after a geometric fit, and outputs TIFF (including LZW).

## 8. Trichrome scanning (`hdrscan/trichrome.go`)
The R, G and B lights are lit one at a time with one shot each (bracketing possible); each frame takes only the matching camera channel, and an RGB TIFF is assembled; the film base is likewise measured with `basestat` and can be sent straight to the DCTL.

## 9. 500T 3200K illuminant balance (v0.89)

The nominal balance of 500T 5219/7219 is 3200K tungsten. Kodak H-1-5219 rev.3-26
gives the spectral sensitivity and ECN-2 characteristic curves. The [transcription record](data/5219_digitization.md)
contains the source, image coordinates, the path to the 10 nm sampled values, and error notes.

### Calculation

Let `L_c(λ)` be the log sensitivity from the Kodak chart, and `S_c(λ)=10^L_c(λ)`.
Sensitivity for missing curve segments is set to zero; blanks are not treated as logS=0.

```text
P(λ,T) ∝ λ^-5 / (exp(c2/(λT)) - 1), c2 = 1.438776877e7 nm K
E_c(T) = Σ S_c(λ) P(λ,T) Δλ, Δλ = 10 nm
q_c = log10(E_c(T)/E_c(3200))
ΔlogH_c = q_c - q_G
ΔD_c = γ_c ΔlogH_c
cast_c = -ΔD_c
```

`P` is normalized at 550 nm. The normalization constant cancels after subtracting `q_G`.
The sensitivity unit is reciprocal energy exposure, so the photon count factor `λ` must not be applied again.
The integration uses only the visible curve samples. Omitting the UV and the undrawn tails is a model limitation.
`γ_R/G/B = 0.4482/0.5266/0.5096` comes from the ECN-2 middle straight-line section,
and is not the creative gamma of Positive_Out. The green exposure is held constant, so `cast_g=0`.

The camera scan still uses the original film base normalization:

```text
D_c = -log10((I_c - black_c) * 2^EV / base_c)
D'_c = D_c + cast_c                 (cast_on = 1)
code_c = (dmin_code + D'_c / dens_step) / full_code
```

By default `cast_on=0` and the offsets are zero. With the correction off, the math is unchanged.
`out_trans=1` is the raw transmittance diagnostic output and does not apply the density offset.
The Density In node of `develop` uses the same formula.

The extra density of the 5500K Planck model is about `[-0.09256, 0, +0.18864] D`,
so the DCTL adds `[+0.09256, 0, -0.18864] D`.
The [complete calculation result](data/5219_5500K_result.json) can be recomputed with the command below:

```sh
go run ./cmd/illuminantcalc -kelvin 5500
```

### Magnitude comparison with 85-series filters

5500K to 3200K is `1e6/3200 - 1e6/5500 = +130.68 mired`.
This is the same color temperature conversion usually labeled 85B.
The common nominal conversion of an 85 is 5500K to 3400K, i.e. +112.30 mired.
Kodak's 500T data sheet recommends an 85 and EI 320 for daylight; this does not mean 85 and 85B
can be treated as the same transmission spectrum. See [Kodak 85-series transmittance curves](https://www.kodak.com/content/products-brochures/Film/Color-Conversion-Filters-85-Series-Spectral-Transmittance.pdf).
This is only a color temperature magnitude check, not an independent verification by spectral integration of a physical 85 filter.

### Accuracy limits

- The values are a model derivation from public sources, not a measured white balance for this roll of negative.
- Planck 5500K and the CIE D55 spectrum differ. LED, fluorescent and mixed light cannot be determined by the K value alone.
- Neutral objects are approximated as flat reflectors. Real object reflectance spectra may produce different offsets.
- Kodak's representative ECN-2 curves are not the processing result of every roll, and the toe and shoulder do not have a constant slope.
- The emulsion layer density axes are not the RGB density axes of an arbitrary camera and backlight. This version maps them approximately,
  without measuring the density crosstalk matrix of the scanning system. The result must not be advertised as exact cast removal.
- If an 85 filter was used when shooting, do not correct again as unfiltered 5500K.
- At 3200K this offset is zero. This only means no illuminant conversion is needed; it does not guarantee the whole inversion chain is naturally neutral.
  The gray point, per-channel gamma and any existing auto white balance of Positive_Out still need separate checks.
- Measured gray points and gray-scale cross cast fitting are left to a later version.

### State and lights

The Light page only calculates, saves and sends DCTL parameters; it does not use the serial port, does not shoot, and does not change exposure.
`colour_cast.json` stores the current export settings. It is independent of the film base preset; on the next export every
Density_In uses this set of settings, and switching film base does not clear the colour cast correction automatically.
`filmdb.Entry.ColourCast` stores the parameters per film stock; after loading they must be written explicitly to take effect.
Film base data keeps its original values; a fake film base is never used to silently replace the density offset.
The log uses the original `leveling_log.csv` format, with mode Colour cast and source recording the density offset;
the zero columns for base and light mean these values were not measured this time, not that the light was set to zero.
