# Product and versioning policy

This repository contains **two independently released products**:

| Product | Git tag pattern | Current tag | Release assets |
|---|---|---|---|
| NegativeScan Studio — Windows scanning app | `vX.YY` | `v1.13` | App EXE and app-specific files |
| NegativeScan Studio OFX — Resolve plugin | `ofx-vX.YY.Z` | `ofx-v0.90.14` | OFX bundle ZIP and plugin-specific files |

A plugin update **does not** bump or rebuild the app; an app update need not rebuild the plugin. Shared-source changes may affect both products, but each gets an explicit release/version decision.

- Release titles identify the product, e.g. **NegativeScan Studio v1.13** and **NegativeScan Studio OFX 0.90.14**.
- The [README](../README.md) links to the current release of each product, rather than relying on GitHub's single repository-wide “latest” badge.
- App history/plan: [changelog](CHANGELOG.md), [roadmap](ROADMAP.md). Plugin history/plan: [changelog](../ofx/CHANGELOG.md), [roadmap](../ofx/ROADMAP.md). Current test/hardware status: [STATUS.md](STATUS.md). How to build and ship: [DEVELOPMENT.md](DEVELOPMENT.md).
- Use plain version numbers in tags, filenames, UI labels, and changelogs. Every version, including a local test build, gets a changelog entry.
- A documentation/repository-metadata-only update needs no binary version bump or new release tag. Do not rewrite existing release tags or quietly replace their binaries to match newer docs.
- Historical exception: `v0.90` initially shipped the app EXE and the first OFX 0.90 ZIP together. Preserve those assets/tags as history. Current plugin downloads use the independent `ofx-v0.90.14` release.
- `nss.scan/1` is the TIFF/sidecar **schema version**, not an app or plugin version. Compatible product releases may share it; a format change requires a separate schema compatibility decision.
- App v0.91 removes DCTL generation/export and app template embedding. Historical tags through v0.90 retain those sources. DCTL is not a third separately versioned product.
- The snapshot tag `archive-2026-11-24` marks the normalised 2026-11-24 tree (app v1.13 + OFX 0.90.14 + documentation). It is a pointer, not a third product, and does not make the GitHub repository read-only.

The root `template.dctl` and `template_core.dctl` remain unchanged as inputs to `ofx/tools/generate.py`. They are not app assets or app export features. Removing them would break OFX regeneration, so app-only cleanup does not delete them.

App v0.91 added the optional `film_base_space` marker to `nss.scan/1`. An absent marker keeps the legacy pre-profile interpretation. `after_profile` identifies an accepted Light-page leveling reference, which needs OFX 0.90.5 or newer. The schema identifier, lamp-drive units, TIFF pixels, and existing keys remain. Existing published release tags and assets stay unchanged.
