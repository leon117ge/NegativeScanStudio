# App v0.91 workflow reference

The app has four modules. Resolve conversion belongs to the independently versioned OFX plugin.

## Camera & Light

Camera connection, live view, clear-base metering, RGB light control, and auto film-base leveling remain available. Scan configuration presets still save and recall camera/light setup. The Light page keeps measured-base display, Copy, and CSV logging, but has no write-to-DCTL button.

**Chart calibration...** is on the Camera page. It fits and saves a camera/light profile from a transmission chart. The profile remains available for leveling and trichrome base measurement. The profile is recorded as pending metadata, not baked into scan TIFF pixels. Calibration DCTL export is removed.

The Light page's minimal 500T illuminant calculator sends offsets only to Develop. It has no global export setting or DCTL-only saved-stock list.

## HDR Scan

White-light and trichrome capture, brackets, Stop, merge, and measured-base output remain available. **Send base to Develop** fills Density In's visible base controls and selects Custom. For trichrome it also turns off mask compensation and crosstalk in the preview. It does not alter TIFF pixels or scan metadata.

TIFF ImageDescription tag 270 still contains the existing `nss.scan/1` JSON. Known light values, measured film base, camera profile/matrix, and scan details keep the same schema and serialization. The software-version value now identifies app v0.91. Missing values are not invented.

## Crop & Merge

Crop, merge files, hair removal, dust cleanup, and batch RAW-to-linear-TIFF conversion remain available. **Open in Develop** opens a merged TIFF for preview. Metadata preservation is unchanged.

## Develop

The built-in negative-to-positive node chain, controls, film-base/neutral picking, roll presets, LUTs, and positive-TIFF export remain available. Conversion math is unchanged. Existing saved base and white-balance lists can still be read for compatibility, and the film-base DB is retained.

The retired DCTL page's file measurement/preset editor, WB export editor, Mask & Crosstalk export settings, generators, and DCTL template embedding are removed. Historical sources remain in tags through v0.90. Existing user files are not deleted or rewritten by this removal.

## Language and compatibility limits

Built-in app labels, tooltips, dialogs, status messages, logs, and defaults use English. User-entered roll/preset names, imported metadata, paths, operating-system dialogs, and device-provided errors can contain other languages. The app does not rename user data or change metadata to enforce English.

The Windows GUI and real camera/light interaction still need hardware acceptance. See [verification](QA_v0.91.md).

The root `template.dctl` and `template_core.dctl` remain unchanged as inputs to `ofx/tools/generate.py`. They are not app assets or app export features. Removing them would break OFX regeneration, so app-only cleanup does not delete them.
