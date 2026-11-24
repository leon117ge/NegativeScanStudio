# Design (historical through v0.90)

> Historical reference through v0.90. App v0.91 removes the DCTL page, all DCTL generation/export, and related write controls. The formulas below remain relevant to the built-in Develop preview, but the old page layout and export instructions are retired. See [current app workflow](APP_WORKFLOW.md), [architecture](ARCHITECTURE.md), [v0.91 changelog](CHANGELOG.md#v091), and [OFX usage](../ofx/README.md).

## Overall architecture

A single Windows executable. The GUI uses lxn/walk (a local fork in `third_party/walk`). Business logic is kept in packages with no UI dependency (`hdrscan`, `lightlevel`, `colorsci`, `develop`, `cropper`, `rgblight`, `camera`, `basestat`, and later `hikcam` / `monocap`) so the algorithms can be tested on Linux (`go test ./...` in each package). The repository root holds pages and glue only.

## Pages (as of v0.90)

- **1. DCTL** (retired in v0.91): film-base measurement (open a TIFF, select a region, `basestat` statistics), a film-base preset table, and export of three DCTLs.
- **2. Camera & Light**: Camera (tether, live view, drag-to-select), Light (RGB Light Air settings, automatic film-base leveling; from v0.88 a shared live view), White light, Trichrome. The four pages share one camera connection and one film-base rectangle (`levelcam.go`).
- **3. HDR Scan**: film-base metering → bracketed capture → merged HDR TIFF; trichrome scanning.
- **4. Crop & Merge**: crop, merge files, hair removal (`hairpage.go`), Develop preview (`develop` package).

Current module layout and the Hikrobot mono path are described in [ARCHITECTURE.md](ARCHITECTURE.md) and [APP_WORKFLOW.md](APP_WORKFLOW.md).

## Data flow (v0.90)

```
X-T3 camera (PTP/WPD) ──RAW──► rawconv (LibRaw, linear 16-bit TIFF)
       ▲                           │
RGB Light Air (serial)             ▼
  White / solo R,G,B ──► hdrscan: meter base → brackets (0/+2/+4 EV) → MergeN → *_HDR.tiff
                         trichrome: R/G/B frames, one channel each → RGB TIFF
                                   │
                                   ▼
          film-base reading (basestat / lightlevel result) → presets / filmdb / scanconfig
                                   │
                                   ▼
 DCTL: FilmScan_Density_In → FilmScan_MaskCrosstalk → FilmScan_Positive_Out (+ 2383 LUT)
       (the develop package reproduces the same chain in float32 Go for preview)
```

The OFX plugin, from 0.90.1, has no mask/crosstalk stage. Resolve development now follows [ARCHITECTURE.md](ARCHITECTURE.md).

## Key conventions

- All film-base numbers are **linear TIFF value / 65535**. Every module uses the same `basestat` method, so a base sent from the HDR module is on the same scale as one measured in the old DCTL module.
- HDR merge is scaled to frame 0; pixels near the film base keep the frame-0 values strictly.
- Leveling changes only the lamp, not camera exposure. The camera uses a fixed ISO / shutter.
- `rgblight` may send only commands listed in the official document; other bytes are rejected (no serial-number write or firmware upgrade).
- Settings and logs live under `%APPDATA%\NegativeScanStudio\` (for example `leveling_log.csv`).

## 500T illuminant balance (v0.89)

`colorsci/illuminant.go` computes nominal density offsets from Kodak sensitivity and a Planck spectrum.
`castpage.go` owned the Light-page controls; `castconfig.go` saved independent export settings.
`filmdb.Entry.ColourCast` stored a film preset. `template.dctl` and `develop/eval.go` used the same density add. The feature defaulted to off and did not change existing scan or export behaviour.

The lamp is only responsible for film-base leveling and is not changed afterwards. Scene-illuminant cast and any later residual-cast correction were intended to happen in the DCTL (now in the OFX plugin / Develop preview). The model does not treat a typical film characteristic as a measurement of this roll. Sources and limits are in the algorithms document.
