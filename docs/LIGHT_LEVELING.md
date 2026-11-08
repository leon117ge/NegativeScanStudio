# Light leveling in v0.92

## Measurement

`xt3Camera.Shoot` decodes each capture with `hdrscan.Load`, the same linear LibRaw path used by `ProcessTrichrome`. `hdrscan.LevelingImage` samples actual pixels on a fixed grid, normally one pixel per 16-pixel interval. Small images use a smaller interval. `lightlevel.Measure` takes the median in the selected rectangle. At least four finite samples are required.

The calibration TIFF uses the same grid, rectangle, channel extraction, and median. This replaces the older combination of a RAW-cell median for control and a separately filtered TIFF statistic for acceptance. A small minority of leaks, dust, or hot samples cannot dominate the median. The rectangle still needs to contain mostly clear film base.

Rectangle uses the user's patch throughout. Auto detects a rectangle once from the first usable demosaiced RGB samples and then freezes it. Auto does not change the measurement region between correction passes.

## Clipping and range

The clipped-sample percentage is diagnostic. It does not control the light update. A base is broadly clipped only when its median is at least 0.98 FS and its 90th percentile is at least 0.995 FS. Broad clipping halves the channel until a usable measurement exists.

A measured code 1 that remains too bright, or a measured code 100 that remains too dark, stops coarse solving with an exposure message. The test allows the raw-space tolerance implied by the inverse camera matrix. The app does not change ISO, shutter, or aperture automatically. ISO and shutter retain the existing fixed-exposure checks.

## Response solver

Let `A` be the profile matrix blended by its enabled strength, and `b` the measured channel medians. The post-profile reference is `A*b`. The continuous raw-space target is `inverse(A)*[target,target,target]`.

Each channel keeps its captured code, reading, and sampled image. Once two usable codes exist, a positive local secant slope estimates the change in level per code. The Jacobian is `A*diag(slopes)`. The implementation solves the equivalent inverse-matrix error followed by division by each measured slope. No black-box matrix probe is needed for independently captured channels.

An initial proportional probe supplies a second measurement. Ordinary updates move at most half the current code, with a minimum step allowance of four codes. A measured bracket bounds the root. A repeated proposal falls back to bisection or an unused adjacent code. History traversal is deterministic.

The current trial controls further exploration. A separate best measured combination is retained for final selection. This prevents a flat response from freezing the solver at an older, equally poor code.

The default coarse solve limit is 10 rounds. A legacy saved limit of four migrates once to 10. Other saved limits remain unchanged, and a newly chosen limit of four remains four.

## Integer refinement and acceptance

Within 20 percent of the target, the solver measures each channel's neighbouring codes. Cached channel frames allow combinations to be evaluated without photographing every combination. The score is the largest relative post-profile error. Squared total error breaks a tie. No interpolated code or predicted level enters the saved result.

If a better neighbouring combination changes the centre, refinement repeats, up to three sweeps. A sweep needs at most six new captures. Every result then gets a fresh full R/G/B capture at the chosen codes. Previously completed channels are not exempt from this final capture.

Default success requires every post-profile channel to lie between 0.49 and 0.51. A coarse integer step does not widen that interval. `Closest reachable within search budget` describes a failed bounded search, not proof of a global hardware optimum.

Real-camera success also requires the calibration TIFF check. One fresh corrective solver pass remains available if that check disagrees. It uses the same locked area and target, rather than inventing a new statistic correction factor. A TIFF conversion or measurement error cannot accept a RAW estimate.

Ten full solve rounds, three neighbour sweeps, and final verification allow at most 51 requested frames per solver pass. The exceptional TIFF correction can run one additional pass. Most runs stop earlier, but capture time and settling have not been measured on the hardware for this build. Linear demosaicing each frame can cost more than the old RAW-plane estimate.

## Saved references and logs

Only an accepted measured result replaces `rgb_light.json`'s leveling reference. A failure or cancellation leaves the previous reference intact. Scan metadata still requires matching lamp codes, and normal scans do not measure scene content as film base. Changing film, exposure, profile, or optics requires a new leveling run.

`leveling_log.csv` retains its successful-run schema. `leveling_failures.csv` records the time in Asia/Shanghai, app version, rectangle, captured codes, after-profile base when all channels exist, profile, failure reason, rounds, target, tolerance, and final RGB capture state. Missing measurements have blank CSV fields rather than invented zeros.

## Hardware limits

The solver assumes each separately lit channel has a locally monotonic response. Noise, PWM modulation, temperature drift, and light movement can break that assumption. Spatial medians and fresh verification detect some effects but do not remove temporal drift. The default settle delay remains 300 ms. Real hardware acceptance is required.
