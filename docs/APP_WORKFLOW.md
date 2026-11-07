# App v0.91 workflow reference

The app has four modules. Resolve conversion belongs to the independently versioned OFX plugin.

## Camera & Light

Camera connection, live view, clear-base metering, RGB light control, and auto film-base leveling remain available. Scan configuration presets still save and recall camera/light setup. The Light page keeps measured-base display, Copy, and CSV logging, but has no write-to-DCTL button.

**Chart calibration...** is on the Camera page. It fits and saves a camera/light profile from a transmission chart. The profile remains available for leveling. The profile is recorded as pending metadata, not baked into scan TIFF pixels. Calibration DCTL export is removed.

The Light page's minimal 500T illuminant calculator sends offsets only to Develop. It has no global export setting or DCTL-only saved-stock list.

## HDR Scan

White-light and trichrome capture, brackets, Stop, and merge remain available.
Both panels have **Film base for metadata: Last leveling (after profile)**,
default on. The panels display the accepted RGB reference and its time in
CST (UTC+8). **Send base to Develop** uses that reference while the option is on.
For trichrome, transfer also turns off preview mask compensation and crosstalk.

A successful Light-page leveling stores its final after-profile base in
`rgb_light.json`. Saving a named or exported scan preset copies the reference
and timestamp into its film data. Loading the preset restores them. Earlier
presets with a valid accepted `tiff_base` can also supply the reference, but
cannot recover a timestamp that was never saved.

Normal captures do not measure the merged image to obtain Film base. The
explicit Light-page leveling verification still measures its calibration TIFFs.
Turning the option off omits `film_base`. Missing, invalid, failed, or mismatched
lamp-drive references also omit it. There is no scene-content or fixed-0.5 fallback.
Re-level after changing the roll, exposure, camera profile, or optical setup.

TIFF ImageDescription tag 270 keeps `nss.scan/1` JSON. App v0.91 writes the
accepted normalized base unchanged, with `base_source: "light leveling (after profile)"`
and additive `film_base_space: "after_profile"`. Lamp drive remains separate in
`light_rgb`. A pending camera matrix is not marked baked into TIFF pixels.
Use independent OFX 0.90.5 for these new references. The plugin does not apply
the matrix to an after-profile base twice, but still applies it to pending pixels.
Unmarked legacy metadata retains its pre-profile meaning. Existing TIFFs are not repaired.

## Crop & Merge

Crop, merge files, hair removal, dust cleanup, and batch RAW-to-linear-TIFF conversion remain available. **Open in Develop** opens a merged TIFF for preview. Metadata preservation is unchanged.

## Develop

The built-in negative-to-positive node chain, controls, film-base/neutral picking, roll presets, LUTs, and positive-TIFF export remain available. Conversion math is unchanged. Existing saved base and white-balance lists can still be read for compatibility, and the film-base DB is retained.

The retired DCTL page's file measurement/preset editor, WB export editor, Mask & Crosstalk export settings, generators, and DCTL template embedding are removed. Historical sources remain in tags through v0.90. Existing user files are not deleted or rewritten by this removal.

## Language and compatibility limits

Built-in app labels, tooltips, dialogs, status messages, logs, and defaults use English. User-entered roll/preset names, imported metadata, paths, operating-system dialogs, and device-provided errors can contain other languages. The app does not rename user data or change metadata to enforce English.

The Windows GUI and real camera/light interaction still need hardware acceptance. See [verification](QA_v0.91.md).

The root `template.dctl` and `template_core.dctl` remain unchanged as inputs to `ofx/tools/generate.py`. They are not app assets or app export features. Removing them would break OFX regeneration, so app-only cleanup does not delete them.
