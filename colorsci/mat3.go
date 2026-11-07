// Package colorsci holds the pure colour-science pieces of Film Base Meter's
// optional "film log" output: Cineon density encoding, orange-mask (film
// base) compensation and dye-crosstalk correction in density space.
//
// Conventions used throughout:
//   - Input values are linear transmittance readings in any fixed scale
//     (Film Base Meter's HDR TIFF is frame-0 scale, 0..65535).
//   - Density is base-10 optical density: D = -log10(T).
//   - "Density above base" is D - D_base = -log10(T / T_base), per channel.
//   - Channel order is always R, G, B.
package colorsci

import (
	"errors"
	"math"
)

// Mat3 is a row-major 3x3 matrix: out[i] = sum_j M[i][j] * in[j].
type Mat3 [3][3]float64

// Identity is the 3x3 identity.
var Identity = Mat3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

// Apply returns m·v.
func (m Mat3) Apply(v [3]float64) [3]float64 {
	return [3]float64{
		m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2],
		m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2],
		m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2],
	}
}

// Mul returns m·n.
func (m Mat3) Mul(n Mat3) Mat3 {
	var r Mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				r[i][j] += m[i][k] * n[k][j]
			}
		}
	}
	return r
}

// Det is the determinant.
func (m Mat3) Det() float64 {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
}

// ErrSingular is returned when a matrix cannot be inverted.
var ErrSingular = errors.New("colorsci: singular matrix")

// Inverse returns m^-1.
func (m Mat3) Inverse() (Mat3, error) {
	d := m.Det()
	if math.Abs(d) < 1e-12 {
		return Mat3{}, ErrSingular
	}
	var r Mat3
	r[0][0] = (m[1][1]*m[2][2] - m[1][2]*m[2][1]) / d
	r[0][1] = (m[0][2]*m[2][1] - m[0][1]*m[2][2]) / d
	r[0][2] = (m[0][1]*m[1][2] - m[0][2]*m[1][1]) / d
	r[1][0] = (m[1][2]*m[2][0] - m[1][0]*m[2][2]) / d
	r[1][1] = (m[0][0]*m[2][2] - m[0][2]*m[2][0]) / d
	r[1][2] = (m[0][2]*m[1][0] - m[0][0]*m[1][2]) / d
	r[2][0] = (m[1][0]*m[2][1] - m[1][1]*m[2][0]) / d
	r[2][1] = (m[0][1]*m[2][0] - m[0][0]*m[2][1]) / d
	r[2][2] = (m[0][0]*m[1][1] - m[0][1]*m[1][0]) / d
	return r, nil
}

// Lerp blends the identity and m: (1-s)·I + s·m. s=0 is "off", s=1 is m.
// This is how the 0/50/100% crosstalk strength is applied.
func (m Mat3) Lerp(s float64) Mat3 {
	var r Mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i][j] = s * m[i][j]
			if i == j {
				r[i][j] += 1 - s
			}
		}
	}
	return r
}

// NormalizeRows scales each row to sum to 1, so an input with equal
// densities in R, G and B (a neutral above base) is returned unchanged and
// the matrix only changes saturation, never the grey balance or contrast.
func (m Mat3) NormalizeRows() Mat3 {
	r := m
	for i := 0; i < 3; i++ {
		s := m[i][0] + m[i][1] + m[i][2]
		if math.Abs(s) > 1e-9 {
			for j := 0; j < 3; j++ {
				r[i][j] = m[i][j] / s
			}
		}
	}
	return r
}

// IsIdentity reports whether m is the identity within eps.
func (m Mat3) IsIdentity(eps float64) bool {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if math.Abs(m[i][j]-Identity[i][j]) > eps {
				return false
			}
		}
	}
	return true
}
