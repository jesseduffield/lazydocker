package asciigraph

import "math"

func minMaxFloat64Slice(v []float64) (min, max float64) {
	min = math.Inf(1)
	max = math.Inf(-1)

	if len(v) == 0 {
		panic("Empty slice")
	}

	for _, e := range v {
		if e < min {
			min = e
		}
		if e > max {
			max = e
		}
	}
	return
}

func round(input float64) float64 {
	if math.IsNaN(input) {
		return math.NaN()
	}
	sign := 1.0
	if input < 0 {
		sign = -1
		input *= -1
	}
	_, decimal := math.Modf(input)
	var rounded float64
	if decimal >= 0.5 {
		rounded = math.Ceil(input)
	} else {
		rounded = math.Floor(input)
	}
	return rounded * sign
}

func linearInterpolate(before, after, atPoint float64) float64 {
	return before + (after-before)*atPoint
}

func interpolateArray(data []float64, fitCount int) []float64 {

	var interpolatedData []float64

	springFactor := float64(len(data)-1) / float64(fitCount-1)
	interpolatedData = append(interpolatedData, data[0])

	for i := 1; i < fitCount-1; i++ {
		spring := float64(i) * springFactor
		before := math.Floor(spring)
		after := math.Ceil(spring)
		atPoint := spring - before
		interpolatedData = append(interpolatedData, linearInterpolate(data[int(before)], data[int(after)], atPoint))
	}
	interpolatedData = append(interpolatedData, data[len(data)-1])
	return interpolatedData
}
