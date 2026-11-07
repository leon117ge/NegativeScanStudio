# NegativeScan Studio app roadmap

Current app: **v0.91** · [App changelog](CHANGELOG.md) · [OFX roadmap](../ofx/ROADMAP.md) · [Versioning](VERSIONING.md)

## Implemented

- Camera and light control, film-base metering and leveling, HDR/trichrome capture and merge, crop, and hair/dust cleanup.
- v0.89 introduced a 5219/500T illuminant model. In v0.91 the app keeps only its Develop-preview calculator, without DCTL export or light changes.
- v0.90 introduced `nss.scan/1` TIFF scan provenance and preservation through image tools. v0.91 keeps tag 270 and the schema, but writes the accepted after-profile Light-page leveling result instead of a scene measurement. Both capture panels show the saved RGB/time and have a persisted default-on metadata switch.
- v0.91 removes the DCTL module, app template embedding, generators, export settings, and cross-module DCTL controls. The app has four modules and English-only built-in UI text.
- Camera chart calibration remains available under Camera & Light > Camera. Develop remains an independent in-app preview and positive-TIFF export tool.

## Next

1. Validate the four-module Windows UI and the complete X-T3/RGB Light Air workflow on real hardware. Check metering, ROI selection, leveling, Stop, scan presets, capture, and metadata accuracy.
2. Improve scan provenance coverage and missing-value diagnostics without inventing old scans' lamp/base values. Keep interoperability with the independent OFX plugin.
3. Validate the app's Develop-preview neutral-reference tools and model error using same-roll neutral steps. A transmission chart alone does not measure emulsion gamma.
4. Research measured scanner spectra, density crosstalk, and CIE daylight models. Keep capture-layer sensitivity distinct from scanner RGB axes.
5. Explore Hikrobot monochrome industrial-camera support. Compatibility and delivery dates are not promised.

DCTL export is retired, not planned work. Historical implementations remain in tags through v0.90. Resolve GPU rendering, viewer picking, and per-stock Base presets belong to the OFX product.

The root `template.dctl` and `template_core.dctl` remain unchanged as inputs to `ofx/tools/generate.py`. They are not app assets or app export features. Removing them would break OFX regeneration, so app-only cleanup does not delete them.
