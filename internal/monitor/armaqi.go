// Package monitor handles device measurement ingestion, evaluation, alert transitions,
// history management (both in-memory caching and persistent storage via JSON / SQL database),
// and notification triggers.
// This file implements processing logic for ArmAQI sensor measurements.
package monitor

import (
	"strconv"

	"github.com/ABespalov/aqinotifier/internal/sensor"
	"github.com/rs/zerolog/log"
)

func (s *MonitorService) processArmAQI(data *sensor.SensorData) {
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
		case "SDS_P1":
			m.PM10Raw = val
			m.PM10 = val
			hasPM10 = true
		case "SDS_P2":
			m.PM25Raw = val
			m.PM25 = val
			hasPM25 = true
		case "BME280_temperature":
			m.TemperatureRaw = val
			m.Temperature = val
		case "BME280_humidity":
			m.HumidityRaw = val
			m.Humidity = val
		case "BME280_pressure":
			pVal := val / 100.0
			m.PressureRaw = pVal
			m.Pressure = pVal
		}
	}

	s.processMeasurementLocked(&m, data, hasPM10, hasPM25)
}
