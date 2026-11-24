# Contributing

NegativeScan Studio is maintained for leon117ge. This short guide is for agents and anyone resuming work after the 2026-11-24 snapshot.

- Read [docs/STATUS.md](docs/STATUS.md) first, then [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
- English only in the app, the plugin, commit messages, and new or revised documentation. Do not add Chinese to app or plugin files.
- Two products, two version lines: app `vX.YY`, plugin `ofx-vX.YY.Z`. Plain versions, a changelog entry for every version, binaries on GitHub Releases (not in git).
- Fast-forward pushes only. Do not force-push, rewrite published tags, or turn this repository into a GitHub read-only archive.
- Do not invent film-base or stock calibration numbers. Do not commit the `wip/metadata-matrix` work or the 400K stash onto `main` without an explicit decision and tests.
- Durable box files belong in `/workspace/nss-archive/` (see its `INDEX.md`). Do not rely on `dist/`, `build/`, `out/`, `target/`, or cache directories.
- Run `./ofx/test.sh` for plugin changes; add `NSS_TEST_OPENCL=1` when kernels change. Run `go test ./...` for app-side packages.

The longer agent checklist lives in [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md#contributing-rules-for-agents).
