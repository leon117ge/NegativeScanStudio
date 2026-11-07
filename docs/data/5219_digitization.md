# H-1-5219 digitization record

Source: https://www.kodak.com/content/pdfs/motion/KODAK-VISION3-500T-5219-7219-technical-information.pdf
Revision 3-26 (March 2026), retrieved 2026-11-07 (Asia/Shanghai).
PDF SHA-256: `6d037d8090373ee50d10bedfff3df14777a575dd107c50edcafc7ec5862d0972`.

Page 4 spectral sensitivity raster: PDF image xref 17, 782×715 pixels.
Axes: x=110 at 250 nm; x=751 at 750 nm. y=560 at logS=0;
y=76 at logS=4. Formula: nm=250+(x-110)*500/641;
logS=(560-y)*4/484. Manually read curve centre lines at 10 nm;
interpolate across overprinted layer labels. R=cyan-forming,
G=magenta-forming, B=yellow-forming. Numeric samples are in
`colorsci/data/5219_log_sensitivity.csv`. Blank means unpublished/outside
visible line extent, NOT logS=0. Integration treats blanks as zero response.
No guessed spectral tails or per-layer re-normalisation are added.
Only 350–710 nm is integrated (ordinary visible-light camera exposure;
UV-rich sources are outside this model). Truncating the visibly unfinished
red edge at 680 nm is a source of systematic uncertainty.

Page 3 sensitometry raster: xref 9, 673×732 pixels. Axes x=85 at
logH=-4, x=652 at logH=1; y=668 at D=0, y=102 at D=3.
Straight-region endpoints x=312 and x=542 correspond to logH=-1.9982
and +0.0300 (span 2.0282). Pixel centre rows (B/G/R) are
405.5/451.0/540.5 at x=312 and 210.5/249.5/369.0 at x=542.
Gamma=(y_left-y_right)*3/566 / ((542-312)*5/567):
R=0.4482, G=0.5266, B=0.5096 (rounded in model).
These are representative ECN-2 midscale slopes, not toe/shoulder curves,
not scanner calibration, and not the Positive_Out creative gamma settings.

Reading uncertainty: approximately ±0.05–0.10 logS over visible curves,
up to ±0.15 in steep/label-obscured sections; ±0.03 in gamma. This is an
engineering estimate, NOT a measured confidence interval. Missing tails,
film processing, actual illuminant SPD, dye/scanner crosstalk and subject
reflectance add unknown systematic errors. No claim of exact WB is made.

Kodak plots are copyrighted by Eastman Kodak; the project provides its
own numeric transcription and source/method, not a license to Kodak art.
