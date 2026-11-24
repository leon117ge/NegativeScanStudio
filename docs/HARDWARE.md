# Hardware

## Hikrobot monochrome camera (current primary path)

Implemented in `hikcam/` (runtime load of `MvCameraControl.dll`, no link-time dependency) and on the HDR Scan pages **Mono Trichrome** and **Focus Check**. See [mono-trichrome.md](mono-trichrome.md) and [ARCHITECTURE.md](ARCHITECTURE.md).

Intended model: Hikrobot MV-CS240-90UM (USB3 monochrome, GMAX2424, 6016 × 4096, Mono 8/10/12). The app enumerates any USB3 or GigE MVS device and always offers a simulator when the SDK or the camera is absent.

App v1.08 was tested on a real MV-CS240-90UM with the RGB Light Air described below. Later app versions (through v1.13) extend that path (shared-exposure leveling, native mono HDR, frames-per-exposure averaging, LiveAE) and still need a formal hardware pass on v1.13 itself; see [STATUS.md](STATUS.md).

`lightlevel.Camera` is the leveling interface used by both this body and the X-T3 path.

## CuteNewDesign RGB Light Air (`rgblight/`)

- USB CDC serial, protocol **RGBAIR API 1.3** (official document: `RGBAIR_API_1.3_official.txt`).
- Frame: `AA 55 | TYPE | SEQ | LEN(u16 LE) | PAYLOAD (starts with CMD) | CRC16 LE`. CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF) covers TYPE through PAYLOAD.
- TYPE: 01 command, 81 response, 82 telemetry, 83 event.
- Only the documented customer commands are implemented: 01 brightness, 02 on/off, 03 set all, 10 mode, 11 speed, 12/13 timing, 14/15 focus light, 20 status, 21, 22. Other commands (serial-number write, firmware upgrade / YMODEM) are rejected.
- From v0.81 the app can record a raw serial log. `rgblight/sim.go` is a simulator for tests.

### RGB spectra (user-provided measurement report, 2026-10-14)

| Channel | peak / nm | FWHM / nm | dominant / nm |
|---|---:|---:|---:|
| R | 650.1 | 22.2 | 633.7 |
| G | 539.9 | 38.7 | 547.6 |
| B | 450.7 | 18.6 | 456.7 |

- Figures come from a user-transcribed measurement report; the original plot attachments were not independently re-read here. No per-wavelength SPD or camera QE was obtained.
- Calculations treat each channel as a Gaussian SPD: `exp(-4 ln(2) ((λ - peak) / FWHM)^2)`. Dominant wavelength is not the spectral peak and must not be substituted for it.
- These wavelengths describe the **scanner lamp**, not the 3200 K / 5500 K scene illuminant the film was shot under. The two must not be mixed in the same calculation. Levelling the three channel intensities on film base only changes scale factors; it is not a gamma calibration.
- A conditional 5219 + this SPD gamma estimate and a crosstalk matrix are recorded in [5219 LED notes](data/5219_led_gamma.md). Nominal R/G/B = 0.453 / 0.536 / 0.527 is not a device measurement and has not been adopted as a global default.

## Fujifilm X-T3 tethering (`camera/`)

- Windows Portable Devices MTP/PTP vendor commands (`wpd_windows.c/.go`); Fujifilm property names live in `fujinames.go`.
- Features: live view, capture and retrieve RAF, ISO and fixed shutter (for example 1/60, 1/120 s; base shutter from v0.81), bracketing (`CaptureBracketEV`, abortable).
- `camera/sim.go` provides a simulated camera for tests.
- This remains a supported capture path (including the 2026-10-31 confirmed 500T roll). The monochrome Hikrobot path is the current primary development focus.
