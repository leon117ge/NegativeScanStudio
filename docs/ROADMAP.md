# NegativeScan Studio app — Roadmap / 扫描软件路线图

Current app: **v0.90** · [App changelog](CHANGELOG.md) · [OFX roadmap](../ofx/ROADMAP.md) · [Versioning](VERSIONING.md)

## Implemented / 已实现

- Camera/light control, film-base metering and leveling, HDR/trichrome capture and merge, crop and hair/dust cleanup.
- v0.89: 5219/500T illuminant model for legacy DCTL export/Develop, based on nominal sensitivity and Planck integration. Lamp settings remain unchanged after leveling; model limits are documented.
- v0.90: `nss.scan/1` TIFF scan provenance and preservation through relevant image tools; tested writer round trips and Windows cross-build.

## Next / 后续

1. Validate the complete Windows capture workflow on real X-T3/RGB Light Air hardware: metering, selected base ROI, leveling, brackets/trichrome, metadata accuracy and manual overrides. Software tests do not replace hardware acceptance.
2. Improve scan provenance coverage and missing-value diagnostics without fabricating old scans' lamp/base information. Keep TIFF/sidecar interoperability with the independent OFX product.
3. Add app-side measured neutral-reference/gray-patch tools for the legacy DCTL export/Develop workflow. The OFX viewer picker is a separate plugin feature, not already an app feature.
4. Fit layer slopes using multiple neutral steps exposed and processed on the same roll; a transmission target by itself does not measure emulsion gamma. Validate 500T model error before stronger accuracy claims or default changes.
5. Research measured scanner spectra, density crosstalk and CIE daylight models, clearly separating capture-layer sensitivity from scanner RGB axes.
6. Explore Hikrobot monochrome industrial-camera support; no delivery date or compatibility promise yet.

Legacy DCTL templates remain available as export/reference tools. Resolve GPU/viewer work, per-stock Base presets and plugin host validation belong to the [OFX roadmap](../ofx/ROADMAP.md). No app version bump is implied by a plugin release.
