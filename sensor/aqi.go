// Package sensor defines data structures and parsing for incoming sensor
// payloads (JSON) received from ESP8266-based sensors.
// This file implements the calculations for Air Quality Index (AQI) using
// data-driven standards loaded from res/aqi.json. Supported standards
// include EU (European CAQI), US (EPA AQI), CN (China AQI), and any
// custom standard added to the configuration file.
package sensor

import (
	"encoding/json"
	"math"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// AQILevel represents one of the air quality levels (1 = best).
// The number of levels varies by standard (e.g. EU has 6, US/CN have 7).
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
	// BreakpointsUS25 holds PM2.5 concentration breakpoints for the US EPA AQI standard.
	BreakpointsUS25 = []float64{9.0, 35.4, 55.4, 125.4, 225.4, 325.4, 500.4}
	// BreakpointsUS10 holds PM10 concentration breakpoints for the US EPA AQI standard.
	BreakpointsUS10 = []float64{54, 154, 254, 354, 424, 504, 604}
	// BreakpointsEU25 holds PM2.5 concentration breakpoints for the European CAQI standard.
	BreakpointsEU25 = []float64{10, 20, 25, 50, 75, 800}
	// BreakpointsEU10 holds PM10 concentration breakpoints for the European CAQI standard.
	BreakpointsEU10 = []float64{20, 40, 50, 100, 150, 1200}

	// IndexPointsUS maps breakpoint intervals to AQI values for the US EPA standard.
	IndexPointsUS = []float64{0, 50, 100, 150, 200, 300, 400, 500}
	// IndexPointsEU maps breakpoint intervals to AQI values for the European CAQI standard.
	IndexPointsEU = []float64{0, 50, 100, 150, 200, 300, 500}

	// standards holds all loaded AQI standards keyed by uppercase tag (e.g. "US", "EU", "CN").
	// Access must be protected by standardsMu.
	standards   map[string]*AQIStandard
	standardsMu sync.RWMutex
)

// AQIZone describes a single air quality zone within a standard, including
// its numeric level, display name, chart color reference, and icon reference.
type AQIZone struct {
	Level int    `json:"level"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

// AQIStandard defines a complete AQI calculation standard loaded from
// res/aqi.json. It contains breakpoints for PM2.5 and PM10, index point
// mappings, rounding rules, and display metadata (zones, flags, names).
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

// GetStandards returns a snapshot of the currently loaded AQI standards map.
// The returned map is safe to iterate without holding a lock.
func GetStandards() map[string]*AQIStandard {
	standardsMu.RLock()
	defer standardsMu.RUnlock()
	// Return a shallow copy to prevent callers from mutating the global map.
	result := make(map[string]*AQIStandard, len(standards))
	for k, v := range standards {
		result[k] = v
	}
	return result
}

// GetStandard returns the AQI standard for the given tag (case-insensitive).
// Returns nil if the standard is not loaded.
func GetStandard(tag string) *AQIStandard {
	standardsMu.RLock()
	defer standardsMu.RUnlock()
	return standards[strings.ToUpper(tag)]
}

// LoadStandards parses a JSON array of AQIStandard definitions and replaces
// the global standards map. This function is safe to call concurrently
// (e.g. during config hot-reload).
func LoadStandards(data []byte) error {
	var list []AQIStandard
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	m := make(map[string]*AQIStandard)
	for i := range list {
		m[strings.ToUpper(list[i].Tag)] = &list[i]
	}
	standardsMu.Lock()
	standards = m
	standardsMu.Unlock()
	return nil
}

// roundValue applies the specified rounding method to val with the given
// number of decimal places. Supported methods: "floor", "round", "ceil".
// If method is empty or "none", the value is returned unchanged.
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
// The higher of the two sub-indices determines the overall AQI and level.
func CalculateAQI(pm25, pm10 float64, standard string) (float64, AQILevel) {
	aqi25, level25 := CalculateValueAQI(pm25, "PM2.5", standard)
	aqi10, level10 := CalculateValueAQI(pm10, "PM10", standard)
	if aqi10 > aqi25 {
		return aqi10, level10
	}
	return aqi25, level25
}

// CalculateValueAQI calculates AQI for a single pollutant value (PM2.5 or PM10)
// using the specified standard tag. The standard is looked up from the loaded
// standards map. If not found, a warning is logged and (0, LevelGood) is returned.
func CalculateValueAQI(val float64, pmType string, standard string) (float64, AQILevel) {
	tag := strings.ToUpper(standard)

	standardsMu.RLock()
	std := standards[tag]
	standardsMu.RUnlock()

	if std == nil {
		log.Warn().Str("standard", standard).Msg("AQI standard not found, returning zero")
		return 0, LevelGood
	}

	var breakpoints []float64
	if pmType == "PM10" {
		val = roundValue(val, std.RoundMethod10, std.RoundDecimals10)
		breakpoints = append([]float64{0}, std.Breakpoints10...)
	} else {
		val = roundValue(val, std.RoundMethod25, std.RoundDecimals25)
		breakpoints = append([]float64{0}, std.Breakpoints25...)
	}

	aqi := calculatePiecewiseAQI(val, breakpoints, std.IndexPoints)
	return aqi, getLevel(aqi, std.IndexPoints)
}

// calculatePiecewiseAQI performs piecewise linear interpolation to convert a
// concentration value (c) into an AQI value using the provided breakpoint and
// index point arrays. If c exceeds the highest breakpoint, the maximum index
// value is returned. If c <= 0, returns 0.
//
// Formula: I = ((Ihigh - Ilow) / (Chigh - Clow)) * (C - Clow) + Ilow
func calculatePiecewiseAQI(c float64, breakpoints []float64, indexPoints []float64) float64 {
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

// getLevel determines the AQILevel for a given AQI value by finding
// which index point interval it falls into. Returns the highest level
// if the value exceeds all breakpoints.
func getLevel(aqi float64, indexPoints []float64) AQILevel {
	for i := 0; i < len(indexPoints)-1; i++ {
		if aqi <= indexPoints[i+1] {
			return AQILevel(i + 1)
		}
	}
	return AQILevel(len(indexPoints) - 1)
}
