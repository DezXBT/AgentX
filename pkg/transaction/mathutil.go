package transaction

import (
	"math"
	"strconv"
)

// roundTo replicates Python's round(x, n). CPython rounds half-to-even on the
// true decimal value; Go's strconv.FormatFloat uses the same round-half-to-even
// rule on the exact binary value, so formatting and re-parsing reproduces
// Python's result bit-for-bit for the inputs this library deals with.
func roundTo(x float64, n int) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
	return v
}

// roundHalfEven replicates Python's round(x) -> int (banker's rounding).
func roundHalfEven(x float64) int {
	v, _ := strconv.ParseInt(strconv.FormatFloat(x, 'f', 0, 64), 10, 64)
	return int(v)
}

// mathRound ports utils.Math.round: floor, bump to ceil on >= .5, keep sign.
func mathRound(num float64) float64 {
	x := math.Floor(num)
	if num-x >= 0.5 {
		x = math.Ceil(num)
	}
	return math.Copysign(x, num)
}

// solve ports ClientTransaction.solve.
func solve(value, minVal, maxVal float64, rounding bool) float64 {
	result := value*(maxVal-minVal)/255 + minVal
	if rounding {
		return math.Floor(result)
	}
	return roundTo(result, 2)
}

// isOdd ports utils.is_odd: -1.0 for odd, 0.0 for even.
func isOdd(n int) float64 {
	if n%2 != 0 {
		return -1.0
	}
	return 0.0
}

// floatToHex ports utils.float_to_hex, including its uppercase A-F digits and
// the leading-dot behaviour for pure fractions.
func floatToHex(x float64) string {
	var result []byte
	quotient := int(x)
	fraction := x - float64(quotient)

	for quotient > 0 {
		quotient = int(x / 16)
		remainder := int(x - float64(quotient)*16)
		if remainder > 9 {
			result = append([]byte{byte(remainder + 55)}, result...)
		} else {
			result = append([]byte{byte(remainder + '0')}, result...)
		}
		x = float64(quotient)
	}

	if fraction == 0 {
		return string(result)
	}

	result = append(result, '.')
	for fraction > 0 {
		fraction *= 16
		integer := int(fraction)
		fraction -= float64(integer)
		if integer > 9 {
			result = append(result, byte(integer+55))
		} else {
			result = append(result, byte(integer+'0'))
		}
	}
	return string(result)
}
