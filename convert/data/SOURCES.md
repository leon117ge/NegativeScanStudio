# Convert D-logH curve sources

Still-film Status M characteristic polylines were extracted earlier by
`tools/digitize_still_gamma.py` from Kodak official PDFs (vector paths) and
live in these CSVs (columns: stock,channel,pdf_x,pdf_y,logH,density).

| Stock | Datasheet | Page / figure | CSV | Notes |
|-------|-----------|---------------|-----|-------|
| Ektar 100 | Kodak E-4046 rev. 1/25 | p.4 daylight Status M characteristic | ektar_characteristic.csv | See docs/data/ofx_stock_gamma.md / ofx_stock_gamma_readings.json |
| Portra 400 | Kodak E-4050 rev. 1/25 | p.4 daylight Status M characteristic | portra_characteristic.csv | same |
| Gold 200 | Kodak E-7022 rev. 06-23 | p.4 daylight Status M characteristic | gold_characteristic.csv | cubic Bezier expanded |
| UltraMax 400 | Kodak E-7023 rev. 2/16 | p.4 daylight Status M characteristic | ultramax_characteristic.csv | same |
| Vision3 500T 5219 | H-1-5219 | p.3 sensitometry (mid-slope only) | — | Full D-logH polyline not digitised; Convert keeps logistic mid-slope from docs/data/5219_digitization.md |
| Vision3 250D 5207 | H-1-5207 | p.3 sensitometry (mid-slope only) | — | Same; docs/data/5207_digitization.md |

Dmin per channel = density at the lowest logH samples (toe / base+mask on the chart).
Convert uses D-Dmin so leveled FS base stays 0.5/0.5/0.5 while channel toe/shoulder shapes remain.
