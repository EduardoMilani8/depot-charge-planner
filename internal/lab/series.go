package lab

import (
	"math"
	"strconv"
)

// Series is a per-minute number series. It is written with one decimal and with null
// for NaN/Inf (no reading, bus not present), which encoding/json cannot do on its own.
type Series []float64

func (s Series) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, len(s)*6+2)
	b = append(b, '[')
	for i, v := range s {
		if i > 0 {
			b = append(b, ',')
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b = append(b, "null"...)
			continue
		}
		r := math.Round(v*10) / 10
		if r == 0 {
			r = 0 // turns -0 into 0
		}
		b = strconv.AppendFloat(b, r, 'f', -1, 64)
	}
	return append(b, ']'), nil
}
