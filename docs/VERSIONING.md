# Product and versioning policy

This repository contains **two independently released products**:

| Product | Git tag pattern | Current tag | Release assets |
|---|---|---|---|
| NegativeScan Studio — Windows scanning app | `vX.YY` | `v0.90` | App EXE and app-specific files |
| NegativeScan Studio OFX — Resolve plugin | `ofx-vX.YY.Z` | `ofx-v0.90.4` | OFX bundle ZIP and plugin-specific files |

`X.YY` follows the project's existing app numbering (the app remains in the 0.9x series). The plugin's `Z` identifies its independent patch release. A plugin update **does not** bump or rebuild the app; an app update need not rebuild the plugin. Shared-source changes may affect both products, but each gets an explicit release/version decision.

- Release titles identify the product, e.g. **NegativeScan Studio app v0.90** and **NegativeScan Studio OFX 0.90.4**.
- The [README](../README.md) links to the current release of each product, rather than relying on GitHub's single repository-wide “latest” badge.
- App history/plan: [changelog](CHANGELOG.md), [roadmap](ROADMAP.md). Plugin history/plan: [changelog](../ofx/CHANGELOG.md), [roadmap](../ofx/ROADMAP.md).
- A documentation/repository-metadata-only update needs no binary version bump or new release tag. Do not rewrite existing release tags or quietly replace their binaries to match newer docs.
- Historical exception: `v0.90` initially shipped the app EXE and the first OFX0.90 ZIP together. Preserve those assets/tags as history. Current plugin downloads use the independent `ofx-v0.90.4` release.
- `nss.scan/1` is the TIFF/sidecar **schema version**, not an app or plugin version. Compatible product releases may share it; a format change requires a separate schema compatibility decision.
- Original DCTL templates remain legacy/reference/export tools. They are not a third separately versioned product.

## 中文

扫描软件用 `vX.YY`，插件用 `ofx-vX.YY.Z`，互不强制同步。目前app v0.90、OFX0.90.4；文档与GitHub简介调整不升级exe/插件。各自维护更新日志和路线图，README分别指向两个release。旧v0.90共同发布的历史附件不删除、不改tag；`nss.scan/1`另属metadata格式版本。
