// Package monitor handles device measurement ingestion, evaluation, alert transitions,
// history management (both in-memory caching and persistent storage via JSON / SQL database),
// and notification triggers.
// This file implements processing logic for AirGradient sensor measurements.
package monitor

import (
	"strconv"

	"github.com/ABespalov/aqinotifier/internal/sensor"
	"github.com/rs/zerolog/log"
)

func (s *MonitorService) processAirGradient(data *sensor.SensorData) {
	s.mu.Lock()

	m := Measurement{
		DeviceID:   data.ParentID,
		Timestamp:  data.DateTime,
		DeviceType: data.DeviceType,
	}

	hasPM10, hasPM25 := false, false
	for _, v := range data.Values {
		val, err := strconv.ParseFloat(v.Value, 64)
		if err != nil {
			log.Warn().Err(err).Str("device", data.ParentID).Str("type", v.Type).Str("value", v.Value).Msg("failed to parse sensor value")
			continue
		}
		switch v.Type {
		case "pm003Count":
			m.PM03Raw = val
			m.PM03 = val
		case "pm01":
			m.PM01Raw = val
			m.PM01 = val
		case "pm02":
			m.PM25Raw = val
			m.PM25 = val
			hasPM25 = true
		case "pm10":
			m.PM10Raw = val
			m.PM10 = val
			hasPM10 = true
		case "rco2":
			m.CO2Raw = val
			m.CO2 = val
		case "atmp":
			m.TemperatureRaw = val
			m.Temperature = val
		case "rhum":
			m.HumidityRaw = val
			m.Humidity = val
		case "tvoc":
			m.TVOCRaw = val
			m.TVOC = val
		case "nox":
			m.NoxRaw = val
			m.Nox = val
		}
	}

	s.processMeasurementLocked(&m, data, hasPM10, hasPM25)
}
