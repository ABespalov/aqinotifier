// Package sensor defines data structures and parsing for incoming sensor
// payloads (JSON) received from ESP8266-based sensors.
// This file implements the calculations for US and EU Air Quality Indexes (AQI).
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
	Tag             string    `json:"tag"`
	NameShort       string    `json:"nameShort"`
	NameFull        string    `json:"nameFull"`
	Flag            string    `json:"flag"`
	IndexPoints     []float64 `json:"indexPoints"`
	Breakpoints25   []float64 `json:"breakpoints25"`
	Breakpoints10   []float64 `json:"breakpoints10"`
	RoundMethod25   string    `json:"roundMethod25"`
	RoundDecimals25 int       `json:"roundDecimals25"`
	RoundMethod10   string    `json:"roundMethod10"`
	RoundDecimals10 int       `json:"roundDecimals10"`
	Zones           []AQIZone `json:"zones"`
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

func roundValue(val float64, method string, decimals int) float64 {
	if method == "none" || method == "" {
		return val
	}
	shift := math.Pow10(decimals)
	switch strings.ToLower(method) {
	case "floor":
		return math.Floor(val*shift) / shift
	case "round":
		return math.Round(val*shift) / shift
	case "ceil":
		return math.Ceil(val*shift) / shift
	default:
		return val
	}
}

// CalculateAQI calculates AQI based on the maximum AQI between PM2.5 and PM10 for a standard.
func CalculateAQI(pm25, pm10 float64, standard string) (float64, AQILevel) {
	aqi25, level25 := CalculateValueAQI(pm25, "PM2.5", standard)
	aqi10, level10 := CalculateValueAQI(pm10, "PM10", standard)
	if aqi10 > aqi25 {
		return aqi10, level10
	}
	return aqi25, level25
}

// CalculateValueAQI calculates AQI for a single value based on standard and PM type.
func CalculateValueAQI(val float64, pmType string, standard string) (float64, AQILevel) {
	tag := strings.ToUpper(standard)
	std, ok := Standards[tag]
	if !ok {
		if tag == "US" {
			std = &AQIStandard{
				Tag:             "US",
				IndexPoints:     IndexPointsUS,
				Breakpoints25:   BreakpointsUS25,
				Breakpoints10:   BreakpointsUS10,
				RoundMethod25:   "floor",
				RoundDecimals25: 1,
				RoundMethod10:   "floor",
				RoundDecimals10: 0,
			}
		} else {
			std = &AQIStandard{
				Tag:             "EU",
				IndexPoints:     IndexPointsEU,
				Breakpoints25:   BreakpointsEU25,
				Breakpoints10:   BreakpointsEU10,
				RoundMethod25:   "none",
				RoundDecimals25: 0,
				RoundMethod10:   "none",
				RoundDecimals10: 0,
			}
		}
	}

	var breakpoints []float64
	if pmType == "PM10" {
		val = roundValue(val, std.RoundMethod10, std.RoundDecimals10)
		breakpoints = append([]float64{0}, std.Breakpoints10...)
	} else {
		val = roundValue(val, std.RoundMethod25, std.RoundDecimals25)
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
