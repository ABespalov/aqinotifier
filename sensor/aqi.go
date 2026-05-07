package sensor

import (
	"math"
	"strings"
)

// AQILevel represents one of the 6 air quality levels.
type AQILevel int

const (
	LevelGood AQILevel = iota + 1
	LevelModerate
	LevelSlightlyUnhealthy
	LevelUnhealthy
	LevelVeryUnhealthy
	LevelHazardous
	LevelExtremelyHazardous
)

var (
	BreakpointsUS25 = []float64{9.0, 35.4, 55.4, 125.4, 225.4, 325.4, 500.4}
	BreakpointsUS10 = []float64{54, 154, 254, 354, 424, 504, 604}
	BreakpointsEU25 = []float64{10, 20, 25, 50, 75, 800}
	BreakpointsEU10 = []float64{20, 40, 50, 100, 150, 1200}

	IndexPointsUS = []float64{0, 50, 100, 150, 200, 300, 400, 500}
	IndexPointsEU = []float64{0, 50, 100, 150, 200, 300, 500}
)

// CalculateUS_AQI calculates the US EPA Air Quality Index for PM2.5 or PM10.
// Based on 2024 standards for PM2.5.
func CalculateUS_AQI(pm25, pm10 float64) (float64, AQILevel) {
	// For PM2.5, EPA truncates to one decimal place
	c25 := math.Floor(pm25*10) / 10
	// For PM10, EPA truncates to integer
	c10 := math.Floor(pm10)

	// Breakpoints for PM2.5 (May 2024): 0-9, 9.1-35.4, 35.5-55.4, 55.5-125.4, 125.5-225.4, 225.5-325.4, 325.5-500.4
	aqi25 := calculateAQI_Piecewise(c25, 
		append([]float64{0}, BreakpointsUS25...),
		IndexPointsUS)
	
	// Breakpoints for PM10: 0-54, 55-154, 155-254, 255-354, 355-424, 425-504, 505-604
	aqi10 := calculateAQI_Piecewise(c10,
		append([]float64{0}, BreakpointsUS10...),
		IndexPointsUS)

	aqi := math.Max(aqi25, aqi10)
	return aqi, getLevel(aqi, IndexPointsUS)
}

// CalculateEU_AQI calculates a continuous index (0-500) based on European EAQI levels.
// Level 1 (Good) -> 0-50
// Level 2 (Fair) -> 51-100
// Level 3 (Moderate) -> 101-150
// Level 4 (Poor) -> 151-200
// Level 5 (Very poor) -> 201-300
// Level 6 (Extremely poor) -> 301-500
func CalculateEU_AQI(pm25, pm10 float64) (float64, AQILevel) {
	// EAQI PM2.5 breakpoints: 0, 10, 20, 25, 50, 75, 800
	aqi25 := calculateAQI_Piecewise(pm25, 
		append([]float64{0}, BreakpointsEU25...),
		IndexPointsEU)
	
	// EAQI PM10 breakpoints: 0, 20, 40, 50, 100, 150, 1200
	aqi10 := calculateAQI_Piecewise(pm10,
		append([]float64{0}, BreakpointsEU10...),
		IndexPointsEU)

	aqi := math.Max(aqi25, aqi10)
	return aqi, getLevel(aqi, IndexPointsEU)
}

// CalculateValueAQI calculates AQI for a single value based on standard and PM type.
func CalculateValueAQI(val float64, pmType string, standard string) (float64, AQILevel) {
	var breakpoints, indexPoints []float64
	if strings.ToUpper(standard) == "US" {
		indexPoints = IndexPointsUS
		if pmType == "PM10" {
			breakpoints = append([]float64{0}, BreakpointsUS10...)
		} else {
			// EPA truncates PM2.5 to one decimal place
			val = math.Floor(val*10) / 10
			breakpoints = append([]float64{0}, BreakpointsUS25...)
		}
	} else {
		indexPoints = IndexPointsEU
		if pmType == "PM10" {
			breakpoints = append([]float64{0}, BreakpointsEU10...)
		} else {
			breakpoints = append([]float64{0}, BreakpointsEU25...)
		}
	}

	aqi := calculateAQI_Piecewise(val, breakpoints, indexPoints)
	return aqi, getLevel(aqi, indexPoints)
}

func calculateAQI_Piecewise(c float64, breakpoints []float64, indexPoints []float64) float64 {
	if c <= 0 {
		return 0
	}

	for i := 0; i < len(breakpoints)-1; i++ {
		if c <= breakpoints[i+1] {
			cLow := breakpoints[i]
			cHigh := breakpoints[i+1]
			iLow := indexPoints[i]
			iHigh := indexPoints[i+1]
			// Linear interpolation formula: I = ((Ihigh - Ilow)/(Chigh - Clow)) * (C - Clow) + Ilow
			return ((iHigh-iLow)/(cHigh-cLow))*(c-cLow) + iLow
		}
	}
	
	// Cap at the maximum index point if above max breakpoint
	return indexPoints[len(indexPoints)-1]
}

func getLevel(aqi float64, indexPoints []float64) AQILevel {
	for i := 0; i < len(indexPoints)-1; i++ {
		if aqi <= indexPoints[i+1] {
			return AQILevel(i + 1)
		}
	}
	return AQILevel(len(indexPoints) - 1)
}
