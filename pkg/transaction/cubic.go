package transaction

// cubic ports cubic_curve.Cubic: a cubic Bézier solver over four control
// values [x1, y1, x2, y2].
type cubic struct {
	curves []float64
}

func newCubic(curves []float64) *cubic { return &cubic{curves: curves} }

func (c *cubic) getValue(t float64) float64 {
	var startGradient, endGradient float64
	start, mid, end := 0.0, 0.0, 1.0

	if t <= 0.0 {
		if c.curves[0] > 0.0 {
			startGradient = c.curves[1] / c.curves[0]
		} else if c.curves[1] == 0.0 && c.curves[2] > 0.0 {
			startGradient = c.curves[3] / c.curves[2]
		}
		return startGradient * t
	}

	if t >= 1.0 {
		if c.curves[2] < 1.0 {
			endGradient = (c.curves[3] - 1.0) / (c.curves[2] - 1.0)
		} else if c.curves[2] == 1.0 && c.curves[0] < 1.0 {
			endGradient = (c.curves[1] - 1.0) / (c.curves[0] - 1.0)
		}
		return 1.0 + endGradient*(t-1.0)
	}

	for start < end {
		mid = (start + end) / 2
		xEst := cubicCalculate(c.curves[0], c.curves[2], mid)
		if abs(t-xEst) < 0.00001 {
			return cubicCalculate(c.curves[1], c.curves[3], mid)
		}
		if xEst < t {
			start = mid
		} else {
			end = mid
		}
	}
	return cubicCalculate(c.curves[1], c.curves[3], mid)
}

func cubicCalculate(a, b, m float64) float64 {
	return 3.0*a*(1-m)*(1-m)*m + 3.0*b*(1-m)*m*m + m*m*m
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
