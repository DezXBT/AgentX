package transaction

import "math"

// interpolate ports interpolate.interpolate (numeric branch only; the boolean
// branch in the Python source is never reached for this library's inputs).
func interpolate(from, to []float64, f float64) []float64 {
	out := make([]float64, len(from))
	for i := range from {
		out[i] = from[i]*(1-f) + to[i]*f
	}
	return out
}

// convertRotationToMatrix ports rotation.convert_rotation_to_matrix.
func convertRotationToMatrix(rotation float64) []float64 {
	rad := rotation * math.Pi / 180
	return []float64{math.Cos(rad), -math.Sin(rad), math.Sin(rad), math.Cos(rad)}
}
