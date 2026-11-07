# Kodak 5207 daylight-model inputs

Official H-1-5207 March 2026: https://www.kodak.com/content/products-brochures/motion-picture/KODAK-VISION3-250D-5207-7207-technical-information.pdf

PDF SHA256: `70adb298a7aabb285d986b720e07c87c27eb2361f0925aea2b15903c08282e16`. Kodak specifies daylight balance at 5500 K.

Spectral sensitivity (page 4) raster: 737×749 px, extracted with pdfimages. Calibration: x=105 at 250 nm, x=693 at 750 nm; y=602 at logS=0, y=158 at logS=4. Hand-read visible curves every 10 nm, saved as `colorsci/data/5207_log_sensitivity.csv`, missing tails set to zero (not logS=0). Curve key: cyan-forming = R, magenta-forming = G, yellow-forming = B. Low tails, overlaps, labels, coarse sampling and truncation create uncertainty; estimate ~0.05 logS at well-defined points, worse in tails. No UV contribution modeled.

Nominal midscale Status M slopes from page 3 sensitometry, **not Kodak tabulated specifications**: x=293 (logH=-1.9), x=429 (logH=-0.7); y at R=530→426, G=447→326, B=402→285; y=640 at D=0, y=100 at D=3. Gamma R/G/B = 0.481481/0.560185/0.541667, roughly ±0.03 engineering reading error. Used only for illuminant density-offset model, not as automatic Positive Out gamma defaults.

This is a representative daylight stock; D does not calibrate all daylight emulsions. All illuminants, including the D 5600 K neutral anchor, use Planck energy spectra (NOT CIE daylight). 5600 rather than Kodak’s 5500 K was selected to give the requested endpoint a zero offset. No assertion of exact camera-space/scanner-space compensation.
