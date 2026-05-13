package sensor

import (
	"encoding/json"
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

	Standards map[string]*AQIStandard
)

type AQIZone struct {
	Level int    `json:"level"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

type AQIStandard struct {
	Tag           string     `json:"tag"`
	NameShort     string     `json:"nameShort"`
	NameFull      string     `json:"nameFull"`
	Flag          string     `json:"flag"`
	IndexPoints   []float64  `json:"indexPoints"`
	Breakpoints25 []float64  `json:"breakpoints25"`
	Breakpoints10 []float64  `json:"breakpoints10"`
	Zones         []AQIZone  `json:"zones"`
}

func LoadStandards(data []byte) error {
	var list []AQIStandard
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	m := make(map[string]*AQIStandard)
	for i := range list {
		m[strings.ToUpper(list[i].Tag)] = &list[i]
	}
	Standards = m
	return nil
}

// CalculateUS_AQI calculates the US EPA Air Quality Index for PM2.5 or PM10.
// Based on 2024 standards for PM2.5.
func CalculateUS_AQI(pm25, pm10 float64) (float64, AQILevel) {
	std, ok := Standards["US"]
	if !ok {
		// Fallback to hardcoded
		c25 := math.Floor(pm25*10) / 10
		c10 := math.Floor(pm10)
		aqi25 := calculateAQI_Piecewise(c25, append([]float64{0}, BreakpointsUS25...), IndexPointsUS)
		aqi10 := calculateAQI_Piecewise(c10, append([]float64{0}, BreakpointsUS10...), IndexPointsUS)
		aqi := math.Max(aqi25, aqi10)
		return aqi, getLevel(aqi, IndexPointsUS)
	}

	c25 := math.Floor(pm25*10) / 10
	c10 := math.Floor(pm10)
	aqi25 := calculateAQI_Piecewise(c25, append([]float64{0}, std.Breakpoints25...), std.IndexPoints)
	aqi10 := calculateAQI_Piecewise(c10, append([]float64{0}, std.Breakpoints10...), std.IndexPoints)
	aqi := math.Max(aqi25, aqi10)
	return aqi, getLevel(aqi, std.IndexPoints)
}

// CalculateEU_AQI calculates a continuous index (0-500) based on European EAQI levels.
// Level 1 (Good) -> 0-50
// Level 2 (Fair) -> 51-100
// Level 3 (Moderate) -> 101-150
// Level 4 (Poor) -> 151-200
// Level 5 (Very poor) -> 201-300
// Level 6 (Extremely poor) -> 301-500
func CalculateEU_AQI(pm25, pm10 float64) (float64, AQILevel) {
	std, ok := Standards["EU"]
	if !ok {
		// Fallback to hardcoded
		aqi25 := calculateAQI_Piecewise(pm25, append([]float64{0}, BreakpointsEU25...), IndexPointsEU)
		aqi10 := calculateAQI_Piecewise(pm10, append([]float64{0}, BreakpointsEU10...), IndexPointsEU)
		aqi := math.Max(aqi25, aqi10)
		return aqi, getLevel(aqi, IndexPointsEU)
	}

	aqi25 := calculateAQI_Piecewise(pm25, append([]float64{0}, std.Breakpoints25...), std.IndexPoints)
	aqi10 := calculateAQI_Piecewise(pm10, append([]float64{0}, std.Breakpoints10...), std.IndexPoints)
	aqi := math.Max(aqi25, aqi10)
	return aqi, getLevel(aqi, std.IndexPoints)
}

// CalculateValueAQI calculates AQI for a single value based on standard and PM type.
func CalculateValueAQI(val float64, pmType string, standard string) (float64, AQILevel) {
	tag := strings.ToUpper(standard)
	std, ok := Standards[tag]
	if !ok {
		// Fallback to legacy hardcoded logic
		var breakpoints, indexPoints []float64
		if tag == "US" {
			indexPoints = IndexPointsUS
			if pmType == "PM10" {
				breakpoints = append([]float64{0}, BreakpointsUS10...)
			} else {
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

	var breakpoints []float64
	if pmType == "PM10" {
		breakpoints = append([]float64{0}, std.Breakpoints10...)
	} else {
		if tag == "US" {
			val = math.Floor(val*10) / 10
		}
		breakpoints = append([]float64{0}, std.Breakpoints25...)
	}

	aqi := calculateAQI_Piecewise(val, breakpoints, std.IndexPoints)
	return aqi, getLevel(aqi, std.IndexPoints)
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
