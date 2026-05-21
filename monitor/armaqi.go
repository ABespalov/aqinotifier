// Package monitor handles device measurement ingestion, evaluation, alert transitions,
// history management (both in-memory caching and persistent storage via JSON / SQL database),
// and notification triggers.
// This file implements processing logic for ArmAQI sensor measurements.
package monitor

import (
	"strconv"
	"strings"

	"github.com/ABespalov/aqinotifier/sensor"
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

	// Apply formulas based on device prefix, mapped name, or global device type
	devName := m.DeviceID
	if n, ok := s.cfg.Monitor.DeviceNames[m.DeviceID]; ok {
		devName = n
	}
	devType := m.DeviceType
	if devType == "" {
		if lm := s.lastMeasurementLocked(m.DeviceID); lm != nil && lm.DeviceType != "" {
			devType = lm.DeviceType
		}
	}
	if devType == "" {
		devType = "ArmAQI"
	}

	for prefix, eval := range s.evaluators {
		if strings.HasPrefix(m.DeviceID, prefix) || strings.HasPrefix(devName, prefix) || strings.HasPrefix(devType, prefix) {
			eval.Evaluate(&m)
			break
		}
	}

	// Safety: don't process measurements without any PM data
	if !hasPM10 && !hasPM25 {
		s.mu.Unlock()
		log.Debug().Str("device", data.ParentID).Msg("skipping measurement with no PM data")
		return
	}

	// Safety: Glitch filter for phantom zeros
	last := s.lastMeasurementLocked(m.DeviceID)
	if last != nil && m.PM10 == 0 && m.PM25 == 0 && (last.PM10 > 0.5 || last.PM25 > 0.5) {
		s.mu.Unlock()
		log.Warn().Str("device", data.ParentID).Float64("prev10", last.PM10).Float64("prev25", last.PM25).Msg("ignoring phantom zero measurement")
		return
	}

	// Calculate diff BEFORE adding to history
	s.calculateDiffLocked(&m)

	// Add to history
	hist := s.history[m.DeviceID]
	hist = append(hist, m)
	s.history[m.DeviceID] = hist

	// Trim history
	s.trimHistoryInternal()

	// Copy measurement for async processing
	mCopy := m
	s.mu.Unlock()

	// Save and notify OUTSIDE of lock
	s.saveHistory(mCopy)
	s.notify(&mCopy)
}
