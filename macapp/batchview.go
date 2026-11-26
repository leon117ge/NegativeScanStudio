//go:build darwin || (linux && gio)

package main

// Crop & Merge > Batch RAW to TIFF (RAWUI.Page in ../rawpage.go).

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"basetool/macapp/cropctl"
	"basetool/rawconv"
)

type batchPage struct {
	u                                            *UI
	c                                            *cropctl.RAWUI
	addFiles, addFolder, remove, clearFin, clear Button
	choose, watch, conv, stop, retry             Button
	destCB, compCB, iccCB, workersCB             Combo
	dng                                          Check
	watchEd                                      Field
	watchSeq                                     int
	table                                        Table
	cards                                        [2]Card
	last                                         int // anchor of a Shift-click
}

func newBatchPage(u *UI) *batchPage { return &batchPage{u: u, c: u.raw, watchSeq: -1, last: -1} }

var batchCols = []tableCol{{"File", 170}, {"Folder", 260}, {"Status", 440}, {"Time", 70}}

func (p *batchPage) layout(gtx layout.Context) layout.Dimensions {
	th, c, hp := p.u.th, p.c, p.u.hp
	if p.watchSeq != c.Seq {
		p.watchSeq = c.Seq
		if p.watchEd.Text() != c.WatchDir() {
			p.watchEd.SetText(c.WatchDir())
		}
	}
	cfg := c.Cfg()
	dest, comp, icc, workers := b2i(cfg.Subfolder), b2i(cfg.LZW), c.ICCIndex(), cfg.Workers-1
	dng := cfg.UseDNG && c.DNGPath != ""
	save := func() { c.SetOptions(dest, comp, icc, workers, dng) }
	return layout.UniformInset(10).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4,
					layout.Rigid(hp.button(&p.addFiles, "Add files...", btnOpts{}, c.AddFilesDialog)),
					layout.Rigid(hp.button(&p.addFolder, "Add folder...", btnOpts{}, c.AddFolderDialog)),
					layout.Rigid(hp.button(&p.remove, "Remove selected", btnOpts{}, c.RemoveSelected)),
					layout.Rigid(hp.button(&p.clearFin, "Clear finished", btnOpts{}, c.ClearFinished)),
					layout.Rigid(hp.button(&p.clear, "Clear", btnOpts{}, c.Clear)),
					layout.Flexed(1, layout.Spacer{}.Layout))
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = gtx.Constraints.Max
				gtx.Constraints.Max.Y = max(gtx.Constraints.Max.Y, gtx.Dp(120))
				return p.table.Layout(gtx, th, batchCols, tableSource{
					rows: c.Rows(),
					cell: c.Cell,
					color: func(r, _ int) color.NRGBA {
						return cellColor(c.CellStyle(r))
					},
					selected: c.Selected,
					click: func(i int, ext, span bool) {
						c.ClickRow(i, ext, span, p.last)
						if !span {
							p.last = i
						}
					},
				})
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Start}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return p.cards[0].Layout(gtx, th, "Output", "", nil, "", func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: 10, Top: 8, Right: 10, Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								row := func(label string, cb *Combo, items []string, sel *int, maxW int, tip string) layout.Widget {
									return func(gtx layout.Context) layout.Dimensions {
										return gridRow(gtx, th, label, 150, func(gtx layout.Context) layout.Dimensions {
											gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(maxW)))
											d, ch := cb.Layout(gtx, th, items, sel, false, tip)
											if ch {
												save()
											}
											return d
										})
									}
								}
								return vbox(gtx,
									row("Save TIFF to", &p.destCB, []string{"Same folder as the RAW file", "\"make_tiff\" subfolder"}, &dest, 260, ""),
									row("Compression", &p.compCB, []string{"None (fastest)", "LZW + predictor (lossless)"}, &comp, 260, ""),
									row("Embedded ICC profile", &p.iccCB, c.ICCNames, &icc, 260, "Only a tag in the file, the pixels are not changed. open_make_tiff default: none."),
									row("Files in parallel", &p.workersCB, c.WorkerItems, &workers, 70, "Each file needs about 0.5 GB of memory while converting."),
									func(gtx layout.Context) layout.Dimensions {
										return gridRow(gtx, th, "Adobe DNG Converter", 150, func(gtx layout.Context) layout.Dimensions {
											return hbox(gtx, 6,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													g := gtx
													if c.DNGPath == "" {
														g = gtx.Disabled()
													}
													d, ch := p.dng.Layout(g, th, "Decode through it", &dng, "RAW > DNG > linear DNG with Adobe's demosaic, then the same LibRaw settings. Falls back to LibRaw if it fails.")
													if ch {
														save()
													}
													return d
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return th.label(gtx, textBody, rgb(130, 130, 130), c.DNGLbl, false, false)
												}))
										})
									},
								)
							})
						})
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return p.cards[1].Layout(gtx, th, "Watch folder (tethered shooting)", "camera", nil, "", func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: 10, Top: 8, Right: 10, Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return vbox(gtx,
									func(gtx layout.Context) layout.Dimensions {
										return hbox(gtx, 6,
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												p.watchEd.ReadOnly = true
												gtx.Constraints.Min.X = gtx.Constraints.Max.X // full width like the walk LineEdit
												d, _ := p.watchEd.Layout(gtx, th, "", "")
												return d
											}),
											layout.Rigid(hp.button(&p.choose, "Choose...", btnOpts{}, c.ChooseWatchDir)))
									},
									layout.Spacer{Height: 6}.Layout,
									func(gtx layout.Context) layout.Dimensions {
										return hbox(gtx, 6,
											layout.Rigid(hp.button(&p.watch, c.WatchText, btnOpts{minW: 120}, c.ToggleWatch)),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, c.WatchLbl) }))
									},
									layout.Spacer{Height: 6}.Layout,
									func(gtx layout.Context) layout.Dimensions {
										return th.text(gtx, "New RAW files saved into this folder (for example by FUJIFILM X Acquire\nor Tether App) are converted automatically with the settings on the left,\nonce the camera has finished writing them. Files already there are ignored.")
									},
								)
							})
						})
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th.Theme, textBody, "Fixed processing (same as open_make_tiff): LibRaw "+rawconv.LibRawVersion()+", camera native RGB (no colour matrix), no white balance (1,1,1,1), gamma 1.0, no auto brightness,\nhighlights unclipped, AHD / X-Trans demosaic, no rotation, 16-bit. Output: <RAW name>.tiff, an existing TIFF is never overwritten.")
				l.Color = cGrey
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hbox(gtx, 4,
					layout.Rigid(hp.button(&p.conv, "Convert to TIFF", btnOpts{primary: true, minW: 220, h: 34, disabled: !c.ConvOn}, c.Convert)),
					layout.Rigid(hp.button(&p.stop, "Stop", btnOpts{h: 34, disabled: !c.StopOn}, c.Stop)),
					layout.Rigid(hp.button(&p.retry, "Retry failed", btnOpts{h: 34}, c.RetryFailed)),
					layout.Flexed(1, layout.Spacer{}.Layout))
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return progressValue(gtx, c.PBVal, c.PBMax) }),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return th.text(gtx, c.Info) }),
		)
	})
}
