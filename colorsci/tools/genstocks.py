# Generate basetool/colorsci/stocks.go from dyes.json (fitdyes.py output).
import json
d = json.load(open('dyes.json'))
meta = [  # Go name, dyes.json key, citation
 ('Ektar100Dyes', 'Ektar 100', 'Kodak E-4046 KODAK PROFESSIONAL EKTAR 100 Film, rev. 1/25, p.4, chart E4046C', 'https://kodakprofessional.com/sites/default/files/2025-07/e4046.pdf'),
 ('Portra400Dyes', 'Portra 400', 'Kodak E-4050 KODAK PROFESSIONAL PORTRA 400 Film, rev. 1/25, p.4', 'https://kodakprofessional.com/sites/default/files/2025-07/e4050.pdf'),
 ('UltraMax400Dyes', 'UltraMax 400', 'Kodak E-7023 KODAK ULTRA MAX 400 Film, rev. 2/16, p.4, chart E7023C', 'https://kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7023_max_400.pdf'),
 ('Gold200Dyes', 'Gold 200', 'Kodak E-7022 KODAK GOLD 200 Film, rev. 06-23, p.4', 'https://www.kodakprofessional.com/sites/default/files/wysiwyg/pro/resources/E7022%20Gold%20tech%20sheet.pdf'),
]
def arr(v):
    return '\n'.join('\t\t\t' + ', '.join('%.3f' % x for x in v[i:i + 10]) + ',' for i in range(0, len(v), 10))
hdr = '''package colorsci

// Per-stock dye sets for the spectral crosstalk model.
//
// Vision3 5219: Kodak H-1-5219 "Spectral Dye Density Curves" (DyeYellow,
// DyeMagenta, DyeCyan in spectral.go): separate Y/M/C curves published.
//
// Sources of the other sets (page 4 "Spectral-Dye-Density Curves"):
'''
for g, k, c, u in meta:
    hdr += '//   %s: %s\n//     %s\n' % (k, c, u)
hdr += '''//
// IMPORTANT: unlike H-1-5219, these four datasheets publish only the TOTAL
// diffuse spectral density of a midscale neutral and of D-min (400-700 nm),
// not separate yellow/magenta/cyan curves. The tables below are therefore
// an ESTIMATE: the chart was digitised (2 nm grid, tools/digitize*.py) and
// the net density (neutral - D-min) fitted (tools/fitdyes.py, rms 0.011-
// 0.013 D) as the 5219 dye shapes with a per-dye peak shift and width
// scale, minus the masking-coupler consumption (shapes of the D-min bands).
// Fitted shift / width per dye (Y, M, C):
'''
for g, k, c, u in meta:
    q = d[k]['params']
    hdr += '//   %-13s Y %+.1f nm x%.2f, M %+.1f nm x%.2f, C %+.1f nm x%.2f (rms %.3f D)\n' % (k + ':', q['sY'], q['wY'], q['sM'], q['wM'], q['sC'], q['wC'], d[k]['rms_net'])
hdr += '''// The decomposition is not unique, and above 700 nm (cyan peak) the curve is
// the shifted 5219 shape, not data. Values are peak-normalised (peak 1),
// sampled 400..800 nm every 5 nm.

// DyeSet is one film stock's image-dye curves.
type DyeSet struct {
	Name, Source string
	Y, M, C      SpectralCurve
}

// Vision3Dyes is the Vision3 5219 set (H-1-5219), the colorsci default.
var Vision3Dyes = DyeSet{Name: "Vision3 5219", Source: "Kodak H-1-5219 p.4",
	Y: DyeYellow, M: DyeMagenta, C: DyeCyan}

func grid5() []float64 {
	g := make([]float64, 0, 81)
	for wl := 400.0; wl <= 800; wl += 5 {
		g = append(g, wl)
	}
	return g
}

var (
'''
for g, k, c, u in meta:
    dy = d[k]['dyes']
    src = c.split(' KODAK')[0] + ' ' + c.split(', ', 1)[1]
    hdr += '\t// %s: estimated from %s.\n\t%s = DyeSet{Name: "%s", Source: "%s (neutral-only chart, decomposed)",\n' % (g, c, g, k, src)
    for ch in 'YMC':
        hdr += '\t\t%s: SpectralCurve{grid5(), []float64{\n%s\n\t\t}},\n' % (ch, arr(dy[ch]))
    hdr += '\t}\n'
hdr += ''')

// Crosstalk is the mask-aware correction matrix for this dye set under the
// same assumptions as DefaultCrosstalk (XT3Approx camera, flat light,
// 0.5 D working point, MaskAware, rows normalised).
func (d DyeSet) Crosstalk() Mat3 {
	inv, err := MaskAware(d.Jacobian(XT3Approx, LightFlat, [3]float64{0.5, 0.5, 0.5})).Inverse()
	if err != nil {
		return Identity
	}
	return inv.NormalizeRows()
}
'''
open('./colorsci/stocks.go', 'w').write(hdr)
