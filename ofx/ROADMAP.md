# NegativeScan Studio OFX — Roadmap

Current plugin: **0.90.2** · [Plugin changelog](CHANGELOG.md) · [Scanning app roadmap](../docs/ROADMAP.md) · [Versioning](../docs/VERSIONING.md)

## Implemented experimentally in 0.90.2

- CUDA Driver/PTX rendering with host contexts/streams, offline assembly verification and no SDK/runtime bundle requirement.
- Multi-context OpenCL program/kernel cache, metadata polling throttle, exact preparation reuse and scoped image requests.
- Visible Gamma scale default1.15 below Gamma B and Exposure default0.90. Stock preset values remain unchanged.
- Real NVIDIA/Resolve execution, CUDA parity and performance are still the first validation tasks, not completed claims.

## Implemented in 0.90.1

- Camera calibration → Density In → Positive Out; mask/crosstalk removed.
- Visible film-base T/D, stock gamma and RGB balance loaders; metadata baseline without compounding, short status, Unicode/classic/BigTIFF loading.
- Source-based viewer gray/white-point sampling with radius; runtime OpenCL buffers and multithreaded CPU fallback.
- Native host/math/metadata tests, actual PoCL CPU OpenCL execution and full-frame CPU benchmark. These are not physical-GPU or real-Resolve acceptance.

## Validation first

1. Real Windows Resolve: plugin discovery, parameter/preset persistence, Unicode file chooser, metadata/manual override and optional source-path extension.
2. Verify native 2D viewer handle, pixel aspect/render-scale/ROI behavior and neutral-patch results on real footage. Consider optional Bake, rectangle selection or tracking only after the fixed-point workflow is validated.
3. Validate OpenCL negotiation under Resolve's GPU modes, device/driver compatibility, CPU retry and actual full-frame performance. Compare against the DCTL workflow without claiming Linux CPU timings are Resolve FPS.

## One measured Base per stock

Each stock will have exactly one `<Stock> Base` loader, independently of its gamma or illuminant choices. Add entries only after measurement, retaining source, conditions and uncertainty.

- **500T Base — implemented:** current R **+1.40**, G **0**, B **+0.80 stops**, user eye-tuned under Gamma scale1.15/Exposure0.90 defaults. The old +1.57/0/+1.00 measurement from 0206 with T5600 and raw gamma .45/.54/.53 is retained as history. No new measurement was performed; T5600 offsets remain unchanged. [Record and table](../docs/data/ofx_film_bases.md).
- **250D / UltraMax400 / Portra400 / Gold200 Base — pending:** no invented or duplicate variants. Existing gamma presets are not evidence of a measured Base calibration.
- Cross-check with same-roll neutral references, multiple exposures/scanner configurations and processed film batches before generalizing calibrations.

## Later candidates, not commitments

- Validate the experimental Windows CUDA path first. Metal/macOS remains a later candidate, subject to host evidence/toolchain feasibility and parity tests. No Metal path or macOS build currently ships.
- More reliable source/sequence metadata integration where the host exposes it; keep explicit TIFF/JSON Load as the portable fallback.
- Measured spectral/CIE daylight models and clearer calibration data management shared with the app, without coupling the products' version numbers.

DCTLs remain legacy/reference tools, not another independently versioned product. The scanning app's hardware/capture work is tracked [separately](../docs/ROADMAP.md).
