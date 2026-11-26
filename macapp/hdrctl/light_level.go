package hdrctl

// Auto film-base leveling, results and the leveling captures: a line by
// line port of lightpage.go startLevel / finishLevelExposure / showResult /
// setImages (Windows v1.13 behaviour: same steps, thresholds, TIFF check,
// corrective pass, final state check, CSV records and handoff).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/lightlevel"
	"basetool/rgblight"
)

func (l *Light) params() (lightlevel.Params, error) {
	var p lightlevel.Params
	_, v, err := l.edits()
	if err != nil {
		return p, err
	}
	tol, err1 := strconv.ParseFloat(strings.TrimSpace(l.TolText), 64)
	it, err2 := strconv.Atoi(strings.TrimSpace(l.IterText))
	st, err3 := strconv.Atoi(strings.TrimSpace(l.SettleText))
	if err1 != nil || tol <= 0 || tol > 20 {
		return p, errors.New("tolerance must be 0.1 .. 20 %")
	}
	if err2 != nil || it < 1 || it > 10 {
		return p, errors.New("max. iterations must be 1 .. 10")
	}
	if err3 != nil || st < 0 || st > 5000 {
		return p, errors.New("settle time must be 0 .. 5000 ms")
	}
	ti := l.cfg.Target
	if ti < 0 || ti >= len(targetVals) {
		ti = 0
	}
	l.cfg.Values, l.cfg.Tol, l.cfg.MaxIter, l.cfg.SettleMS, l.cfg.Target = v, tol, it, st, ti
	applyProfile := !l.cfg.NoCam
	if l.UsesMonoCam() {
		applyProfile = false
	} else {
		l.cfg.NoCam = !l.CamProfile
		applyProfile = !l.cfg.NoCam
	}
	l.saveCfg()
	pr := lightlevel.Params{Target: targetVals[ti], Tol: tol / 100, MaxIter: it, Settle: time.Duration(st) * time.Millisecond, Area: l.cfg.Area, Start: v}
	pl, err := camPlanFor(pr.Target, applyProfile)
	if err != nil {
		return p, err
	}
	if pl.on {
		pr.Matrix = pl.m.Lerp(pl.k)
	}
	l.lastPlan = pl
	return pr, nil
}

// StopLevel is the Stop button.
func (l *Light) StopLevel() {
	if l.cancel != nil {
		l.cancel()
	}
}

// StartLevel is the Start leveling button.
func (l *Light) StartLevel() {
	if !l.connected() || l.busy {
		return
	}
	const title = "Auto film-base leveling"
	p, err := l.params()
	if err != nil {
		l.msg(title, err.Error())
		return
	}
	var baseNorm hdrscan.Rect
	if box, ok := l.manualBase(); ok {
		p.Area = box
		baseNorm = toScanRect(box)
		l.publishRect(box)
	} else {
		p.Area = lightlevel.Centre
		if l.cfg.AreaMode == 1 {
			l.msg(title, "Film base is set to Rectangle: drag a rectangle over a small patch of clear film base on the preview (or on Camera live view) first, or choose Auto.")
			return
		}
	}
	c := l.cl
	h := l.hdr
	restore := [3]bool{true, true, true}
	if s := c.CachedState(); s != nil {
		restore = s.On
		if restore == [3]bool{} {
			restore = [3]bool{true, true, true} // levelled light for scanning: all on
		}
	}
	var cam lightlevel.Camera
	worker := l.lw
	tmpDir := ""
	iso := isoVals[clampIdx(l.cfg.ISO, len(isoVals))]
	realCam := false
	src := l.camSource()
	if src == camSrcHik && l.sim != nil {
		l.msg(title, "A real Hikrobot camera cannot see the simulated RGB light.\n\nPick Simulated Hikrobot MV-CS240-90UM as the leveling camera, or connect the real light.")
		return
	}
	var hikSetup *hikLevelCamera
	wantHikSim := src == camSrcHikSim
	if src == camSrcHik || src == camSrcHikSim {
		l.stopHikPreview()
		td, terr := os.MkdirTemp("", "nss_leveling_*")
		if terr != nil {
			l.msg(title, "Cannot create a temp folder for the leveling frames:\n\n"+terr.Error())
			return
		}
		tmpDir = td
		hikSetup = &hikLevelCamera{lights: l.hikLights(), wantUS: l.hikExposureUS()}
		cam = hikSetup
	} else if l.sim != nil {
		sd := l.sim
		syn := lightlevel.NewSynth(sd.Lights)
		syn.Exp = camera.Exposure{ISO: iso, Shutter: h.fixedShutter()}.String()
		cam = syn
	} else {
		if h.dev == nil || h.isSim || h.sess == nil {
			l.msg(title, "Connect the X-T3 on Camera and Light > Camera first (USB tether, M mode).\n\nThe simulated HDR camera cannot see the light.\n\nOr pick a Hikrobot monochrome camera (or its simulator) as the leveling camera.")
			return
		}
		if h.Busy {
			l.msg(title, "The camera is busy (metering, scan or leveling).")
			return
		}
		h.sess.Fixed = h.fixedShutter()
		// v0.83: leveling RAWs are temp files - never in the scan folder
		td, terr := os.MkdirTemp("", "nss_leveling_*")
		if terr != nil {
			l.msg(title, "Cannot create a temp folder for the leveling frames:\n\n"+terr.Error())
			return
		}
		tmpDir = td
		cam = &xt3Camera{sess: h.sess, iso: iso, dir: td}
		worker, realCam = h.worker, true
		h.pauseLiveView()
		h.Busy = true
	}
	if baseNorm.Empty() {
		p.SelectArea = hdrscan.FindLevelingArea
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.lastSuccess = nil
	l.saveCfg()
	l.busy, l.level = true, true
	l.lastTgt = p.Target
	l.ResText = "Leveling ..."
	l.log("==== Auto film-base leveling: target %.2f FS, tolerance ±%.1f%%, max %d rounds, settle %v, start R %d G %d B %d ====",
		p.Target, p.Tol*100, p.MaxIter, p.Settle, p.Start[0], p.Start[1], p.Start[2])
	if !baseNorm.Empty() {
		l.log("Film base: manual rectangle %.0f-%.0f%% x %.0f-%.0f%% (fixed samples and median for control and TIFF check).",
			baseNorm.X0*100, baseNorm.X1*100, baseNorm.Y0*100, baseNorm.Y1*100)
	} else {
		l.log("Film base: detect once from the initial RGB captures, then lock the same rectangle for control and TIFF check.")
	}
	pl := l.lastPlan
	if src == camSrcHik || src == camSrcHikSim {
		l.log("Apply camera profile: disabled for the monochrome camera (no Bayer CFA, no CameraCal matrix). Each raw film-base channel is leveled to %.2f.", pl.want)
	} else if pl.on {
		l.log("Apply camera profile: %s - target is the base AFTER the matrix (%.2f/%.2f/%.2f), i.e. %s before it.", pl.label(), pl.want, pl.want, pl.want, f3(pl.pre))
	} else {
		l.log("Apply camera profile: off (or no profile enabled) - target %.2f in the plain trichrome TIFF.", pl.want)
	}
	sess := h.sess
	fixedSh := h.fixedShutter()
	hik := h.Hik
	worker.do(func() {
		defer cancel()
		if tmpDir != "" {
			defer func() {
				n := countFiles(tmpDir)
				if rerr := os.RemoveAll(tmpDir); rerr != nil {
					l.log("Leveling temp folder %s not fully removed: %v", tmpDir, rerr)
				} else {
					l.log("Leveling temp frames deleted (%d files in %s) - nothing kept in the scan folder.", n, tmpDir)
				}
			}()
		}
		if hikSetup != nil {
			hik.stopPreviewSync()
			d, herr := hik.ensureHik(wantHikSim)
			if herr != nil {
				l.ui(func() {
					l.busy, l.level, l.cancel = false, false, nil
					l.ResText = "Leveling failed: " + herr.Error()
					l.msg(title, "Cannot open the Hikrobot monochrome camera:\n\n"+herr.Error()+"\n\nConnect it on HDR Scan > Mono trichrome, or pick Simulated Hikrobot MV-CS240-90UM.")
				})
				return
			}
			hikSetup.dev = d
			l.log("Hikrobot leveling uses a single common exposure (gain 0 dB, Mono12, full frame) from the Exposure field / Mono Trichrome scan settings; ISO and shutter are not used.")
		}
		l.log("Camera: %s", cam.Name())
		res, err := lightlevel.Run(ctx, busyOnceLight{c}, cam, p, restore, func(s string) { l.log("%s", s) })
		// v0.85: check in the real trichrome TIFF (same converter and
		// clear-base box as Density_In's auto base), one corrective pass
		post, inTIFF := pl.post(res.Readings), false
		if !res.Area.Empty() {
			baseNorm = toScanRect(res.Area)
		}
		if hk, ok := cam.(*hikLevelCamera); ok && err == nil && res.Converged() {
			for pass := 0; pass < 2; pass++ {
				l.log("TIFF check %d: merging the last R/G/B monochrome leveling frames with the mono trichrome pipeline ...", pass+1)
				base, n, verr := hikTiffBase(ctx, hk, tmpDir, baseNorm)
				if verr != nil {
					err = fmt.Errorf("final TIFF verification unavailable: %w; no new base accepted", verr)
					l.log("%v", err)
					break
				}
				post, inTIFF = pl.post(base), true
				l.log("TIFF check %d: film base in the mono trichrome TIFF %s (%d samples); after %s: %s; worst %.1f%% from %.2f.",
					pass+1, f3(base), n, pl.label(), f3(post), 100*pl.worstDev(post), pl.want)
				if pl.worstDev(post) <= p.Tol || pass == 1 {
					break
				}
				p2 := p
				p2.Start = res.Values
				p2.Area, p2.SelectArea = res.Area, nil
				l.log("Correcting: fresh measured-response pass after TIFF verification; same locked pixels, statistic and 0.5-per-channel target.")
				r2, e2 := lightlevel.Run(ctx, busyOnceLight{c}, cam, p2, restore, func(s string) { l.log("%s", s) })
				res, err = r2, e2
				post, inTIFF = pl.post(res.Readings), false
				if err != nil || !res.Converged() {
					break
				}
			}
		}
		if x, ok := cam.(*xt3Camera); ok && err == nil && res.Converged() {
			for pass := 0; pass < 2; pass++ {
				l.log("TIFF check %d: converting the last R/G/B leveling RAWs with the trichrome scan pipeline ...", pass+1)
				base, n, verr := tiffBase(ctx, x.last, tmpDir, baseNorm)
				if verr != nil {
					err = fmt.Errorf("final TIFF verification unavailable: %w; no new base accepted", verr)
					l.log("%v", err)
					break
				}
				post, inTIFF = pl.post(base), true
				l.log("TIFF check %d: film base in the trichrome TIFF %s (%d samples); after %s: %s; worst %.1f%% from %.2f.",
					pass+1, f3(base), n, pl.label(), f3(post), 100*pl.worstDev(post), pl.want)
				if pl.worstDev(post) <= p.Tol || pass == 1 {
					break
				}
				p2 := p
				p2.Start = res.Values
				p2.Area, p2.SelectArea = res.Area, nil
				l.log("Correcting: fresh measured-response pass after TIFF verification; same locked pixels, statistic and after-profile target.")
				r2, e2 := lightlevel.Run(ctx, busyOnceLight{c}, cam, p2, restore, func(s string) { l.log("%s", s) })
				res, err = r2, e2
				post, inTIFF = pl.post(res.Readings), false
				if err != nil || !res.Converged() {
					break
				}
			}
		}
		if err == nil && res.Converged() && inTIFF && pl.worstDev(post) > p.Tol {
			err = fmt.Errorf("closest reachable: final TIFF base is still %.2f%% from target (tolerance %.2f%%); no new base accepted", 100*pl.worstDev(post), 100*p.Tol)
		}
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		st, serr := c.ReadState()
		if err == nil && res.Converged() {
			if serr != nil {
				err = fmt.Errorf("cannot confirm final light state: %w", serr)
			} else if st.Value != res.Values {
				err = fmt.Errorf("final light state does not match measured levels: got %v, measured %v", st.Value, res.Values)
			}
		}
		var record *levelingRecord
		if err == nil && res.Converged() {
			mode, source := "Auto", "Control verification"
			if p.SelectArea == nil {
				mode = "Rectangle"
			}
			if inTIFF {
				source = "TIFF-checked"
			}
			if !realCam {
				if _, hik := cam.(*hikLevelCamera); !hik {
					source = "Simulated measurement"
				}
			}
			record = &levelingRecord{Time: time.Now(), Version: appVersion, Mode: mode, Box: baseNorm,
				Values: res.Values, Base: post, Profile: pl.label(), Source: source, Measured: inTIFF || !realCam}
			if e := appendLevelingLog(filepath.Join(configDir(), "leveling_log.csv"), *record); e != nil {
				l.log("WARNING: leveling_log.csv could not be appended: %v", e)
			}
		}
		if record == nil {
			why := strings.Join(res.Problems(), "; ")
			if err != nil {
				why = err.Error()
			}
			attempt := levelingRecord{Time: time.Now(), Version: appVersion, Mode: "Rectangle", Box: baseNorm, Values: res.Values, Base: post, Profile: pl.label(), Source: "Failed or incomplete verification"}
			if p.SelectArea != nil {
				attempt.Mode = "Auto"
			}
			if e := appendLevelingFailure(filepath.Join(configDir(), "leveling_failures.csv"), attempt, res, p.Target, p.Tol, why); e != nil {
				l.log("WARNING: leveling_failures.csv could not be appended: %v", e)
			} else {
				l.log("Failed attempt recorded in leveling_failures.csv: %s", why)
			}
		}
		released := false
		if sess != nil && sess.Locked != nil && (realCam || err == nil) {
			sess.Locked, released = nil, true // the light changed: frame 0 is locked again (handoff) or metered by hand
		}
		l.ui(func() {
			l.busy, l.level, l.cancel = false, false, nil
			if released {
				h.clearLocked()
			}
			if realCam {
				h.Busy = false
				h.resumeLiveView()
			}
			if serr == nil {
				l.showState(st, true)
			}
			l.lastPost, l.lastTIFF = post, inTIFF
			l.lastSuccess = record
			l.cfg.Leveling = acceptedLeveling(l.cfg.Leveling, record)
			if record != nil {
				l.cfg.Values = res.Values
			}
			l.saveCfg()
			expDesc, hikMode, exp := l.finishLevelExposure(cam, iso, fixedSh, res)
			if hikMode && err == nil && res.Converged() {
				h.recordLeveledBase(l.hikExposureUS(), res.Values, time.Now())
			}
			l.showResult(res, err, expDesc, hikMode)
			if err == nil && res.Converged() && realCam {
				// v0.82: hand the levelled light + exposure to the frame-0 lock (X-T3 path only)
				h.takeHandoff(baseHandoff{ISO: exp.ISO, Shutter: exp.Shutter, Values: res.Values, Source: "RGB light leveling " + time.Now().Format("15:04:05")})
			} else if released {
				h.say("Light changed on the Light page - no new base accepted; frame 0 lock released, meter the base again.")
			}
		})
	})
}

// finishLevelExposure applies a changed Hikrobot exposure to the shared
// exposure and builds the result-dialog exposure text (UI goroutine).
func (l *Light) finishLevelExposure(cam lightlevel.Camera, iso int, fixedSh uint32, res lightlevel.Result) (desc string, hik bool, exp camera.Exposure) {
	if x, ok := cam.(*xt3Camera); ok {
		exp = x.exp
		l.lastExp = exp
		return exp.String(), false, exp
	}
	if hk, ok := cam.(*hikLevelCamera); ok {
		us, gain := hk.GetExposure()
		desc = fmt.Sprintf("%.0f us, %.2f dB", us, gain)
		l.hdr.setSharedHikExposure(us, "leveling")
		if res.Fit.Changed {
			l.log("Hikrobot exposure saved as the shared exposure: %.0f us, 0.00 dB (was %.0f us).", us, res.Fit.StartedUS)
		}
		return desc, true, exp
	}
	exp = camera.Exposure{ISO: iso, Shutter: fixedSh}
	l.lastExp = exp
	return exp.String(), false, exp
}

func (l *Light) showResult(res lightlevel.Result, err error, expDesc string, hik bool) {
	const title = "Auto film-base leveling"
	l.setImages(res.Images, l.lastTgt, res)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			l.log("Leveling stopped.")
			l.ResText = fmt.Sprintf("Stopped. Light left at R %d G %d B %d.", res.Values[0], res.Values[1], res.Values[2])
		} else {
			l.log("Leveling FAILED: %v", err)
			l.ResText = "Failed: " + err.Error()
			if errors.Is(err, camera.ErrNotManual) {
				l.hdr.notManualMsg(err)
			} else {
				l.msg(title, "Leveling failed:\n\n"+err.Error())
			}
		}
		return
	}
	r := res
	l.lastRes = &r
	pl := l.lastPlan
	src := "predicted"
	if l.lastTIFF {
		src = "TIFF-checked"
	}
	prof := "no profile"
	if pl.on {
		prof = "after profile"
	}
	if hik {
		prof = "raw (no profile)"
	}
	t := fmt.Sprintf("R %d G %d B %d -> base %.5f/%.5f/%.5f linear FS (%s, %s); box %.3f/%.3f/%.3f FS, %d rounds, %s",
		res.Values[0], res.Values[1], res.Values[2], l.lastPost[0], l.lastPost[1], l.lastPost[2], prof, src,
		res.Readings[0], res.Readings[1], res.Readings[2], res.Rounds, expDesc)
	if hik && res.Fit.Changed {
		t += fmt.Sprintf("; exposure changed from %.0f us to %.0f us (gain 0 dB) and saved as the shared Hikrobot exposure", res.Fit.StartedUS, res.Fit.FinalUS)
	}
	pr := res.Problems()
	if len(pr) == 0 {
		t = "LEVELLED: " + t
	} else {
		t = "NOT LEVELLED: " + t + "\n" + strings.Join(pr, "\n")
	}
	l.ResText = t
	l.log("Result: %s (%s)", t, pl.label())

	limited := res.Limited[0] || res.Limited[1] || res.Limited[2]
	tooBright := res.TooBright[0] || res.TooBright[1] || res.TooBright[2]
	if !limited && !tooBright {
		return
	}
	msg := strings.Join(pr, "\n") + "\n\n"
	if hik {
		if res.Fit.Changed {
			msg += fmt.Sprintf("The Hikrobot exposure was changed from %.0f us to %.0f us (gain 0 dB) and saved as the shared exposure for leveling and Mono Trichrome.", res.Fit.StartedUS, res.Fit.FinalUS)
		} else {
			msg += "The Hikrobot exposure was NOT changed (it stays at " + expDesc + ")."
		}
		captureNote := "Mono Trichrome capture uses this same shared exposure for R, G and B (HDR brackets are EV offsets from it)."
		if l.hdr.hikMeterEach() {
			captureNote = "Auto-meter each channel is enabled in Mono Trichrome, so capture starts from this exposure and meters each channel separately."
		}
		if limited {
			msg += "\n\nTo reach the target: increase the Hikrobot exposure (us) in the leveling settings, or open the aperture. Keep gain at 0 dB. " + captureNote
		} else {
			msg += "\n\nThe light is too strong for this exposure even at brightness 1: reduce the Hikrobot exposure (us), stop the lens down, or choose a higher base level. Keep gain at 0 dB. " + captureNote
		}
	} else {
		msg += "The camera exposure was NOT changed (it stays at " + expDesc + ")."
		if limited {
			msg += "\n\nTo reach the target: choose a higher ISO in the leveling settings (then use the same ISO for HDR Fixed frame 0), open the aperture, or choose a lower base level. You can also keep this result and save it as it is."
		} else {
			msg += "\n\nThe light is too strong for this exposure even at brightness 1: choose a lower ISO, stop the lens down, or a higher base level."
		}
	}
	l.msg(title, msg)
}

// ---------- preview of the last leveling captures ----------

func (l *Light) setImages(im [3]*lightlevel.Image, target float64, res lightlevel.Result) {
	if c := lightlevel.Composite(im, target); c != nil {
		l.Comp = c
	}
	for k := 0; k < 3; k++ {
		if g := lightlevel.Gray(im[k], target); g != nil {
			l.Thumbs[k] = g
			l.ThumbTxt[k] = fmt.Sprintf("%s only, brightness %d: base %.3f", rgblight.ChannelNames[k], valueShot(res, k), res.Readings[k])
		}
	}
	if l.Comp != nil {
		l.ViewLbl = "Top: the three captures combined (target = mid grey).\nMagenta = clipped. Drag to move the measure area."
	}
	l.ImgSeq++
}

// CanDrag reports whether the leveling-captures preview takes a drag.
func (l *Light) CanDrag() bool { return !l.level && l.Comp != nil }

// PickArea is the end of a drag on the leveling-captures preview.
func (l *Light) PickArea(r lightlevel.Rect) {
	if r.X1-r.X0 > 0.03 && r.Y1-r.Y0 > 0.03 {
		l.publishRect(r)
		l.log("Film base rectangle set to %.0f-%.0f%% x %.0f-%.0f%% of the frame (used for leveling measure and TIFF check).", r.X0*100, r.X1*100, r.Y0*100, r.Y1*100)
	}
}

// Copy is the Copy button (the accepted leveling as text).
func (l *Light) Copy() {
	if l.lastSuccess == nil {
		return
	}
	l.hdr.Host.SetClipboard(l.lastSuccess.text())
}
