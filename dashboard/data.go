// Package dashboard handles loading layouts, managing visual endpoints,
// preparing telemetry and rendering dashboard screens to EPD, PNG, or BMP.
package dashboard

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
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

// LoadLanguageDict parses local translation dictionaries (e.g. res/ru.json)
// falling back to English (res/en.json) for missing keys.
func LoadLanguageDict(lang string) map[string]string {
	dict := make(map[string]string)

	// 1. Try loading English dictionary as base
	enPath := filepath.Join("res", "en.json")
	if bytes, err := os.ReadFile(enPath); err == nil {
		var enDict map[string]string
		if err := json.Unmarshal(bytes, &enDict); err == nil {
			for k, v := range enDict {
				dict[k] = v
			}
			log.Info().Str("file", enPath).Msg("dashboard: loaded base translations from additional file")
		}
	}

	// 2. Load specified language dictionary and overwrite English keys
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang != "en" && lang != "" {
		langPath := filepath.Join("res", lang+".json")
		if bytes, err := os.ReadFile(langPath); err == nil {
			var langDict map[string]string
			if err := json.Unmarshal(bytes, &langDict); err == nil {
				for k, v := range langDict {
					dict[k] = v
				}
				log.Info().Str("file", langPath).Msg("dashboard: loaded localized translations from additional file")
			}
		}
	}

	return dict
}

// BuildTelemetryMap populates a generic data map containing values, labels, units,
// and custom calculated values for the layout engine to render.
func BuildTelemetryMap(m *monitor.Measurement, appCfg *config.Config, localDict map[string]string) map[string]interface{} {
	telemetry := map[string]interface{}{
		"device_id":               m.DeviceID,
		"device_type":             m.DeviceType,
		"pm25":                    m.PM25,
		"pm25.value":              m.PM25,
		"pm25_raw":                m.PM25Raw,
		"pm10":                    m.PM10,
		"pm10.value":              m.PM10,
		"pm10_raw":                m.PM10Raw,
		"temp.value.to_celsius":   m.Temperature,
		"temperature.value":       m.Temperature,
		"humidity.value":          m.Humidity,
		"pressure.value.to_mm_hg": m.Pressure * hPaToMmHg,
		"pressure.value":          m.Pressure,
		"dew_point.value":         CalcDewPoint(m.Temperature, m.Humidity),
		"pm25.label":              localDict["labelPm25"],
		"pm10.label":              localDict["labelPm10"],
		"temp.label":              localDict["msgTemp"],
		"humidity.label":          localDict["msgHum"],
		"pressure.label":          localDict["msgPress"],
		"dew_point.label":         localDict["msgDewPoint"],
	}

	// Calculate AQI and localized zone levels/names for US, EU, and CN standards
	for _, stdTag := range []string{"US", "EU", "CN"} {
		tag := strings.ToLower(stdTag)
		aqiVal, aqiLevel := sensor.CalculateAQI(m.PM25, m.PM10, stdTag)

		telemetry["aqi."+tag+".value"] = aqiVal
		telemetry["aqi."+tag+".zone.index"] = float64(aqiLevel)

		// Set localized AQI zone name
		zoneName := ""
		if s := sensor.GetStandard(stdTag); s != nil && int(aqiLevel) > 0 && int(aqiLevel) <= len(s.Zones) {
			zoneName = s.Zones[aqiLevel-1].Name
		}
		// Resolve dynamic i18n lookup keys (e.g. aqiNameL1Us, aqiNameL2Eu)
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
		if localized, ok := localDict[i18nKey]; ok {
			zoneName = localized
		}
		telemetry["aqi."+tag+".zone.name"] = zoneName

		// Calculate PM pollutant levels under each standard
		_, pm25Level := sensor.CalculateValueAQI(m.PM25, "PM2.5", stdTag)
		_, pm10Level := sensor.CalculateValueAQI(m.PM10, "PM10", stdTag)
		telemetry["pm25."+tag+".zone.index"] = float64(pm25Level)
		telemetry["pm10."+tag+".zone.index"] = float64(pm10Level)
	}

	// Setup default "aqi" mapping based on standard defined in application config
	defaultStd := "US"
	if appCfg.Monitor.AQI.Standard != "" {
		defaultStd = appCfg.Monitor.AQI.Standard
	}
	defaultTag := strings.ToLower(defaultStd)
	telemetry["aqi"] = telemetry["aqi."+defaultTag+".value"]
	telemetry["aqi.value"] = telemetry["aqi."+defaultTag+".value"]
	telemetry["aqi.zone.index"] = telemetry["aqi."+defaultTag+".zone.index"]
	telemetry["aqi.zone.name"] = telemetry["aqi."+defaultTag+".zone.name"]

	return telemetry
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
