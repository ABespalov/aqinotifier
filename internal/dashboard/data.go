// Package dashboard handles loading layouts, managing visual endpoints,
// preparing telemetry and rendering dashboard screens to EPD, PNG, or BMP.
package dashboard

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/internal/config"
	"github.com/ABespalov/aqinotifier/internal/monitor"
	"github.com/ABespalov/aqinotifier/internal/sensor"
)

const (
	// Magnus-Tetens formula constants for dew point calculation
	magnusB = 17.27
	magnusC = 237.7

	// Conversion constant from hPa (hectopascals) to mmHg (millimeters of mercury)
	hPaToMmHg = 0.750064
)

// CalcDewPoint computes the dew point temperature (in Celsius) given air temperature (t, in Celsius)
// and relative humidity (rh, in percentage, e.g. 55.0).
func CalcDewPoint(t, rh float64) float64 {
	if rh <= 0 {
		return 0.0
	}
	gamma := math.Log(rh/100.0) + (magnusB*t)/(magnusC+t)
	return (magnusC * gamma) / (magnusB - gamma)
}



// TelemetryProvider implements csirender.DataProvider for lazy evaluation of parameters.
type TelemetryProvider struct {
	m         *monitor.Measurement
	appCfg    *config.Config
	localDict map[string]string
	charts    map[string][]float64
}

func NewTelemetryProvider(m *monitor.Measurement, appCfg *config.Config, localDict map[string]string, charts map[string][]float64) *TelemetryProvider {
	return &TelemetryProvider{
		m:         m,
		appCfg:    appCfg,
		localDict: localDict,
		charts:    charts,
	}
}

func (p *TelemetryProvider) GetValue(key string) (interface{}, bool) {
	switch key {
	case "device_id":
		return p.m.DeviceID, true
	case "device_type":
		return p.m.DeviceType, true
	case "pm25", "pm25.value":
		return p.m.PM25, true
	case "pm25_raw":
		return p.m.PM25Raw, true
	case "pm10", "pm10.value":
		return p.m.PM10, true
	case "pm10_raw":
		return p.m.PM10Raw, true
	case "temp.value.to_celsius", "temperature.value":
		return p.m.Temperature, true
	case "humidity.value":
		return p.m.Humidity, true
	case "pressure.value.to_mm_hg":
		return p.m.Pressure * hPaToMmHg, true
	case "pressure.value":
		return p.m.Pressure, true
	case "dew_point.value":
		return CalcDewPoint(p.m.Temperature, p.m.Humidity), true
	case "pm25.label":
		return p.localDict["labelPm25"], true
	case "pm10.label":
		return p.localDict["labelPm10"], true
	case "temp.label":
		return p.localDict["msgTemp"], true
	case "humidity.label":
		return p.localDict["msgHum"], true
	case "pressure.label":
		return p.localDict["msgPress"], true
	case "dew_point.label":
		return p.localDict["msgDewPoint"], true
	case "unitCelsius", "celsius", "temperature", "temp.value.to_celsius.unit", "temperature.value.unit", "dew_point.value.unit":
		return p.localDict["unitC"], true
	case "unitPercent", "percent", "humidity.value.unit":
		if p.localDict["unitHum"] != "" {
			return p.localDict["unitHum"], true
		}
		return "%", true
	case "unitMmhg", "mmhg", "pressure.value.to_mm_hg.unit":
		return p.localDict["unitMmhg"], true
	case "unitPm", "pm", "pm25.value.unit", "pm10.value.unit", "pm25.unit", "pm10.unit":
		return p.localDict["unitPm"], true
	case "pressure.value.unit":
		return p.localDict["unitHpa"], true
	}

	parts := strings.Split(key, ".")
	if len(parts) >= 1 && parts[0] == "aqi" && len(parts) < 3 {
		defaultStd := "US"
		if p.appCfg.Monitor.AQI.Standard != "" {
			defaultStd = p.appCfg.Monitor.AQI.Standard
		}
		stdTag := strings.ToLower(defaultStd)
		if key == "aqi" || key == "aqi.value" {
			return p.GetValue("aqi." + stdTag + ".value")
		}
		if key == "aqi.zone.index" {
			return p.GetValue("aqi." + stdTag + ".zone.index")
		}
		if key == "aqi.zone.name" {
			return p.GetValue("aqi." + stdTag + ".zone.name")
		}
	}

	if len(parts) >= 3 {
		metric := parts[0]
		stdTag := strings.ToUpper(parts[1])
		prop := strings.Join(parts[2:], ".")

		if metric == "aqi" {
			aqiVal, aqiLevel := sensor.CalculateAQI(p.m.PM25, p.m.PM10, stdTag)
			if prop == "value" {
				return aqiVal, true
			}
			if prop == "zone.index" {
				return float64(aqiLevel), true
			}
			if prop == "zone.name" {
				zoneName := ""
				if s := sensor.GetStandard(stdTag); s != nil && int(aqiLevel) > 0 && int(aqiLevel) <= len(s.Zones) {
					zoneName = s.Zones[aqiLevel-1].Name
				}
				var stdSuffix string
				switch stdTag {
				case "US":
					stdSuffix = "Us"
				case "EU":
					stdSuffix = "Eu"
				case "CN":
					stdSuffix = "Cn"
				default:
					stdSuffix = stdTag
				}
				i18nKey := fmt.Sprintf("aqiNameL%d%s", aqiLevel, stdSuffix)
				if localized, ok := p.localDict[i18nKey]; ok {
					zoneName = localized
				}
				return zoneName, true
			}
		}

		if metric == "pm25" {
			_, pm25Level := sensor.CalculateValueAQI(p.m.PM25, "PM2.5", stdTag)
			if prop == "zone.index" {
				return float64(pm25Level), true
			}
		}

		if metric == "pm10" {
			_, pm10Level := sensor.CalculateValueAQI(p.m.PM10, "PM10", stdTag)
			if prop == "zone.index" {
				return float64(pm10Level), true
			}
		}
	}

	return nil, false
}

func (p *TelemetryProvider) GetChart(source string) ([]float64, bool) {
	ch, ok := p.charts[source]
	return ch, ok
}

func (p *TelemetryProvider) ResolveThreshold(condition string, chartSource string) (float64, bool) {
	return ResolveThresholdValue(condition, chartSource)
}

func (p *TelemetryProvider) EnrichTelemetry(source string, value float64) map[string]interface{} {
	return EnrichChartTelemetry(source, value)
}

// ExtractValue extracts a numeric property from a Measurement based on name.
func ExtractValue(m monitor.Measurement, source string, standard string) float64 {
	source = strings.ToLower(strings.TrimSpace(source))
	if strings.Contains(source, "aqi") {
		std := standard
		if strings.Contains(source, "us") || strings.Contains(source, "usa") {
			std = "US"
		} else if strings.Contains(source, "eu") {
			std = "EU"
		} else if strings.Contains(source, "cn") {
			std = "CN"
		}
		val, _ := sensor.CalculateAQI(m.PM25, m.PM10, std)
		return val
	}
	if strings.Contains(source, "pm25") {
		return m.PM25
	}
	if strings.Contains(source, "pm10") {
		return m.PM10
	}
	if strings.Contains(source, "temp") || strings.Contains(source, "temperature") {
		return m.Temperature
	}
	if strings.Contains(source, "humidity") || strings.Contains(source, "hum") {
		return m.Humidity
	}
	if strings.Contains(source, "pressure") || strings.Contains(source, "press") {
		return m.Pressure
	}
	if strings.Contains(source, "dew") {
		return CalcDewPoint(m.Temperature, m.Humidity)
	}
	return 0.0
}

// ResampleHistory resamples historical measurement records into a fixed number of chart columns (points).
// It aggregates values within dynamically calculated intervals (duration/points) using averages.
func ResampleHistory(hist []monitor.Measurement, duration time.Duration, points int, source string, standard string) []float64 {
	res := make([]float64, points)
	if len(hist) == 0 {
		return res
	}

	endTime := time.Now()
	startTime := endTime.Add(-duration)
	bucketDuration := duration / time.Duration(points)

	for i := 0; i < points; i++ {
		bStart := startTime.Add(bucketDuration * time.Duration(i))
		bEnd := bStart.Add(bucketDuration)

		var sum float64
		var count float64
		for _, m := range hist {
			if (m.Timestamp.After(bStart) || m.Timestamp.Equal(bStart)) && m.Timestamp.Before(bEnd) {
				val := ExtractValue(m, source, standard)
				sum += val
				count++
			}
		}

		if count > 0 {
			res[i] = sum / count
		} else {
			// Forward fill from previous point, or fallback to nearest available measurement
			if i > 0 {
				res[i] = res[i-1]
			} else {
				res[i] = ExtractValue(hist[0], source, standard)
			}
		}
	}
	return res
}

// EnrichChartTelemetry derives additional telemetry keys for a chart source and
// value pair so that per-bar and per-label threshold conditions can reference
// zone.index without depending on the global live telemetry value.
//
// The standard tag is extracted from the source name itself: for a source like
// "aqi.us.value" or "pm25.eu.zone.index", the segment between the first and
// second dots is used as the standard tag and looked up via sensor.GetStandard.
// This works with any number of standards loaded from JSON — no hardcoded names.
//
// Example: source="aqi.us.value", value=65.0 → {"aqi.us.zone.index": 2}
//
// This function is intended to be used as csirender.TelemetryEnricher.
func EnrichChartTelemetry(source string, value float64) map[string]interface{} {
	result := make(map[string]interface{})

	// Extract the standard tag from the source name.
	// Sources follow the pattern "<metric>.<stdtag>.<property>", e.g. "aqi.us.value".
	// The tag is the segment between the first and second dots.
	parts := strings.SplitN(source, ".", 3)
	if len(parts) < 2 {
		return result
	}
	metric := strings.ToLower(parts[0])
	tag := strings.ToLower(parts[1]) // e.g. "us", "eu", "cn", or any future standard

	std := sensor.GetStandard(strings.ToUpper(tag))
	if std == nil {
		return result
	}

	var zoneIndex int

	switch {
	case metric == "aqi":
		// Determine zone from AQI IndexPoints breakpoints
		for i, bp := range std.IndexPoints {
			if value >= bp {
				zoneIndex = i + 1
			}
		}
		result["aqi."+tag+".zone.index"] = float64(zoneIndex)

	case metric == "pm25" || metric == "pm2.5":
		// Determine zone from PM2.5 breakpoints
		for i, bp := range std.Breakpoints25 {
			if value >= bp {
				zoneIndex = i + 2 // breakpoints start at zone 2
			}
		}
		result["pm25."+tag+".zone.index"] = float64(zoneIndex)

	case metric == "pm10":
		// Determine zone from PM10 breakpoints
		for i, bp := range std.Breakpoints10 {
			if value >= bp {
				zoneIndex = i + 2
			}
		}
		result["pm10."+tag+".zone.index"] = float64(zoneIndex)
	}

	return result
}

// ResolveThresholdValue resolves a comparison condition like "aqi.us.zone.index == 2"
// to the concrete Y-axis value (e.g. 50.0) that marks the boundary of that zone.
// Used by the chart engine to draw horizontal threshold lines at the correct position.
func ResolveThresholdValue(cond string, chartSource string) (float64, bool) {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return 0, false
	}
	condParts := strings.Split(cond, " ")
	if len(condParts) != 3 {
		return 0, false
	}
	varName := condParts[0]
	rightStr := condParts[2]

	var level int
	if _, err := fmt.Sscanf(rightStr, "%d", &level); err != nil {
		return 0, false
	}

	// Extract the standard tag from the condition variable name.
	// Variable names follow the pattern "<metric>.<stdtag>.<property>",
	// e.g. "aqi.us.zone.index". If that fails, fall back to the chart source.
	var tag string
	varSegments := strings.SplitN(varName, ".", 3)
	if len(varSegments) >= 2 {
		tag = strings.ToUpper(varSegments[1])
	}
	if tag == "" {
		srcSegments := strings.SplitN(chartSource, ".", 3)
		if len(srcSegments) >= 2 {
			tag = strings.ToUpper(srcSegments[1])
		}
	}

	std := sensor.GetStandard(tag)
	if std == nil {
		return 0, false
	}

	metric := strings.ToLower(varSegments[0])
	switch {
	case metric == "aqi":
		if level >= 1 && level <= len(std.IndexPoints) {
			return std.IndexPoints[level-1], true
		}
	case metric == "pm25" || metric == "pm2.5":
		if level >= 2 && level-2 < len(std.Breakpoints25) {
			return std.Breakpoints25[level-2], true
		}
	case metric == "pm10":
		if level >= 2 && level-2 < len(std.Breakpoints10) {
			return std.Breakpoints10[level-2], true
		}
	}

	return 0, false
}
