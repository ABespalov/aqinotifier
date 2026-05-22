package storage

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/rs/zerolog/log"
)

func (s *Storage) initMonitorTableLocked() {
	if s.db == nil {
		return
	}

	query := `
	CREATE TABLE IF NOT EXISTS measurements (
		device_id TEXT,
		timestamp TIMESTAMPTZ,
		pm10 DOUBLE PRECISION,
		pm25 DOUBLE PRECISION,
		pm10_raw DOUBLE PRECISION DEFAULT 0,
		pm25_raw DOUBLE PRECISION DEFAULT 0,
		temperature DOUBLE PRECISION,
		humidity DOUBLE PRECISION,
		pressure DOUBLE PRECISION,
		device_type TEXT DEFAULT 'ArmAQI',
		pm03 DOUBLE PRECISION DEFAULT 0,
		pm03_raw DOUBLE PRECISION DEFAULT 0,
		pm01 DOUBLE PRECISION DEFAULT 0,
		pm01_raw DOUBLE PRECISION DEFAULT 0,
		co2 DOUBLE PRECISION DEFAULT 0,
		co2_raw DOUBLE PRECISION DEFAULT 0,
		tvoc DOUBLE PRECISION DEFAULT 0,
		tvoc_raw DOUBLE PRECISION DEFAULT 0,
		nox DOUBLE PRECISION DEFAULT 0,
		nox_raw DOUBLE PRECISION DEFAULT 0,
		temperature_raw DOUBLE PRECISION DEFAULT 0,
		humidity_raw DOUBLE PRECISION DEFAULT 0,
		pressure_raw DOUBLE PRECISION DEFAULT 0
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_measurements_unique ON measurements (device_id, timestamp);
	CREATE INDEX IF NOT EXISTS idx_measurements_device_time ON measurements (device_id, timestamp);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		log.Error().Err(err).Msg("storage: failed to initialize SQL table for measurements")
		return
	}

	// Migrations
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pm10_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pm25_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN device_type TEXT DEFAULT 'ArmAQI'")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pm03 DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pm03_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pm01 DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pm01_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN co2 DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN co2_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN tvoc DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN tvoc_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN nox DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN nox_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN temperature_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN humidity_raw DOUBLE PRECISION DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE measurements ADD COLUMN pressure_raw DOUBLE PRECISION DEFAULT 0")

	// Backfill
	_, _ = s.db.Exec("UPDATE measurements SET pm10_raw = pm10 WHERE pm10_raw = 0 AND pm10 != 0")
	_, _ = s.db.Exec("UPDATE measurements SET pm25_raw = pm25 WHERE pm25_raw = 0 AND pm25 != 0")
}

func (s *Storage) syncMonitorJSON() {
	s.monitorFileMu.Lock()
	defer s.monitorFileMu.Unlock()

	s.mu.RLock()
	jsonFile := s.cfg.Database.File.Json
	db := s.db
	s.mu.RUnlock()

	if jsonFile == "" || db == nil {
		return
	}

	data, err := os.ReadFile(jsonFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Err(err).Str("file", jsonFile).Msg("storage: failed to read JSON for sync")
		}
		return
	}

	var all []monitor.Measurement
	if err := json.Unmarshal(data, &all); err != nil {
		log.Error().Err(err).Str("file", jsonFile).Msg("storage: failed to unmarshal JSON for sync")
		return
	}

	if len(all) == 0 {
		return
	}

	log.Info().Int("count", len(all)).Msg("storage: syncing JSON measurements to SQL...")

	tx, err := db.Begin()
	if err != nil {
		log.Error().Err(err).Msg("storage: failed to begin sync transaction")
		return
	}
	defer tx.Rollback()

	query := `
		INSERT INTO measurements (
			device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw, temperature, humidity, pressure, device_type,
			pm03, pm03_raw, pm01, pm01_raw, co2, co2_raw, tvoc, tvoc_raw, nox, nox_raw, temperature_raw, humidity_raw, pressure_raw
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		ON CONFLICT (device_id, timestamp) DO NOTHING`

	stmt, err := tx.Prepare(query)
	if err != nil {
		log.Error().Err(err).Msg("storage: failed to prepare sync statement")
		return
	}
	defer stmt.Close()

	for _, m := range all {
		_, err = stmt.Exec(
			m.DeviceID, m.Timestamp, m.PM10, m.PM25, m.PM10Raw, m.PM25Raw, m.Temperature, m.Humidity, m.Pressure, m.DeviceType,
			m.PM03, m.PM03Raw, m.PM01, m.PM01Raw, m.CO2, m.CO2Raw, m.TVOC, m.TVOCRaw, m.Nox, m.NoxRaw, m.TemperatureRaw, m.HumidityRaw, m.PressureRaw,
		)
		if err != nil {
			log.Error().Err(err).Msg("storage: failed to execute sync statement for a measurement")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Msg("storage: failed to commit sync transaction")
		return
	}

	log.Info().Int("count", len(all)).Msg("storage: sync completed, clearing JSON file")

	emptyJSON, _ := json.Marshal([]monitor.Measurement{})
	if err := os.WriteFile(jsonFile, emptyJSON, 0644); err != nil {
		log.Error().Err(err).Str("file", jsonFile).Msg("storage: failed to clear JSON file after sync")
	}
}

// LoadMeasurements loads the history from JSON and/or SQL depending on configuration.
func (s *Storage) LoadMeasurements(limit int) map[string][]monitor.Measurement {
	history := make(map[string][]monitor.Measurement)

	s.mu.RLock()
	hasJSON := s.cfg.Database.HasUse("json")
	jsonFile := s.cfg.Database.File.Json
	db := s.db
	dbConnected := s.dbConnected
	s.mu.RUnlock()

	// 1. Load from JSON if enabled
	if hasJSON && jsonFile != "" {
		s.monitorFileMu.RLock()
		data, err := os.ReadFile(jsonFile)
		s.monitorFileMu.RUnlock()

		if err == nil {
			var all []monitor.Measurement
			if err := json.Unmarshal(data, &all); err == nil {
				// Backfill
				for i := range all {
					if all[i].PM10Raw == 0 && all[i].PM10 != 0 {
						all[i].PM10Raw = all[i].PM10
					}
					if all[i].PM25Raw == 0 && all[i].PM25 != 0 {
						all[i].PM25Raw = all[i].PM25
					}
					if all[i].DeviceType == "" {
						all[i].DeviceType = "ArmAQI"
					}
				}
				for _, m := range all {
					history[m.DeviceID] = append(history[m.DeviceID], m)
				}
			}
		}
	}

	// 2. Load from SQL if connected
	if dbConnected && db != nil {
		rows, err := db.Query("SELECT DISTINCT device_id FROM measurements")
		if err == nil {
			var deviceIDs []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					deviceIDs = append(deviceIDs, id)
				}
			}
			rows.Close()

			for _, id := range deviceIDs {
				mRows, err := db.Query(`
					SELECT device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw, temperature, humidity, pressure, COALESCE(device_type, 'ArmAQI'),
					       pm03, pm03_raw, pm01, pm01_raw, co2, co2_raw, tvoc, tvoc_raw, nox, nox_raw, temperature_raw, humidity_raw, pressure_raw
					FROM measurements 
					WHERE device_id = $1 
					ORDER BY timestamp DESC 
					LIMIT $2`, id, limit)
				if err != nil {
					continue
				}

				var sqlHist []monitor.Measurement
				for mRows.Next() {
					var m monitor.Measurement
					if err := mRows.Scan(
						&m.DeviceID, &m.Timestamp, &m.PM10, &m.PM25, &m.PM10Raw, &m.PM25Raw, &m.Temperature, &m.Humidity, &m.Pressure, &m.DeviceType,
						&m.PM03, &m.PM03Raw, &m.PM01, &m.PM01Raw, &m.CO2, &m.CO2Raw, &m.TVOC, &m.TVOCRaw, &m.Nox, &m.NoxRaw, &m.TemperatureRaw, &m.HumidityRaw, &m.PressureRaw,
					); err == nil {
						sqlHist = append([]monitor.Measurement{m}, sqlHist...)
					}
				}
				mRows.Close()

				existing := history[id]
				merged := append(existing, sqlHist...)

				unique := make(map[int64]monitor.Measurement)
				for _, m := range merged {
					unique[m.Timestamp.Unix()] = m
				}

				var final []monitor.Measurement
				for _, m := range unique {
					final = append(final, m)
				}
				sort.Slice(final, func(i, j int) bool {
					return final[i].Timestamp.Before(final[j].Timestamp)
				})

				history[id] = final
			}
		}
	}

	// 3. Trim all device histories to limit
	if limit > 0 {
		for id, hist := range history {
			if len(hist) > limit {
				history[id] = hist[len(hist)-limit:]
			}
		}
	}

	return history
}

// SaveMeasurement saves a single measurement depending on dual mode settings.
func (s *Storage) SaveMeasurement(m monitor.Measurement) {
	s.mu.RLock()
	hasSQL := s.cfg.Database.DBProvider() != ""
	hasJSON := s.cfg.Database.HasUse("json")
	dbConnected := s.dbConnected && s.db != nil
	db := s.db
	jsonFile := s.cfg.Database.File.Json
	maxValues := s.cfg.Database.MaxValues
	s.mu.RUnlock()

	saveJSON := func(prune bool) {
		s.monitorFileMu.Lock()
		defer s.monitorFileMu.Unlock()

		if jsonFile == "" {
			return
		}

		data, err := os.ReadFile(jsonFile)
		var all []monitor.Measurement
		if err == nil {
			_ = json.Unmarshal(data, &all)
		}

		all = append(all, m)

		if prune && maxValues > 0 {
			byDevice := make(map[string][]monitor.Measurement)
			for _, val := range all {
				byDevice[val.DeviceID] = append(byDevice[val.DeviceID], val)
			}
			var pruned []monitor.Measurement
			for _, list := range byDevice {
				if len(list) > maxValues {
					list = list[len(list)-maxValues:]
				}
				pruned = append(pruned, list...)
			}
			all = pruned
			sort.Slice(all, func(i, j int) bool {
				return all[i].Timestamp.Before(all[j].Timestamp)
			})
		}

		out, err := json.MarshalIndent(all, "", "  ")
		if err == nil {
			_ = os.WriteFile(jsonFile, out, 0644)
		}
	}

	saveSQL := func() {
		if db == nil {
			return
		}
		// Goroutine to not block
		go func() {
			query := `
				INSERT INTO measurements (
					device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw, temperature, humidity, pressure, device_type,
					pm03, pm03_raw, pm01, pm01_raw, co2, co2_raw, tvoc, tvoc_raw, nox, nox_raw, temperature_raw, humidity_raw, pressure_raw
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`
			_, err := db.Exec(query,
				m.DeviceID, m.Timestamp, m.PM10, m.PM25, m.PM10Raw, m.PM25Raw, m.Temperature, m.Humidity, m.Pressure, m.DeviceType,
				m.PM03, m.PM03Raw, m.PM01, m.PM01Raw, m.CO2, m.CO2Raw, m.TVOC, m.TVOCRaw, m.Nox, m.NoxRaw, m.TemperatureRaw, m.HumidityRaw, m.PressureRaw,
			)
			if err != nil {
				log.Error().Err(err).Msg("storage: failed to save to SQL")
			}
		}()
	}

	if !hasSQL && hasJSON {
		saveJSON(true)
	} else if hasSQL && !hasJSON {
		saveSQL()
	} else if hasSQL && hasJSON {
		if dbConnected {
			saveSQL()
		} else {
			saveJSON(false)
		}
	}
}

func (s *Storage) GetMeasurementsByDuration(deviceID string, duration time.Duration) []monitor.Measurement {
	s.mu.RLock()
	hasSQL := s.cfg.Database.DBProvider() != ""
	hasJSON := s.cfg.Database.HasUse("json")
	dbConnected := s.dbConnected && s.db != nil
	db := s.db
	jsonFile := s.cfg.Database.File.Json
	s.mu.RUnlock()

	since := time.Now().UTC().Add(-duration)

	if hasSQL && dbConnected && db != nil {
		query := `SELECT device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw,
		temperature, humidity, pressure, COALESCE(device_type, 'ArmAQI'),
		pm03, pm03_raw, pm01, pm01_raw, co2, co2_raw,
		tvoc, tvoc_raw, nox, nox_raw,
		temperature_raw, humidity_raw, pressure_raw
		FROM measurements
		WHERE device_id = $1 AND timestamp >= $2
		ORDER BY timestamp ASC`
		rows, err := db.Query(query, deviceID, since)
		if err == nil {
			var res []monitor.Measurement
			for rows.Next() {
				var m monitor.Measurement
				if err := rows.Scan(
					&m.DeviceID, &m.Timestamp, &m.PM10, &m.PM25, &m.PM10Raw, &m.PM25Raw,
					&m.Temperature, &m.Humidity, &m.Pressure, &m.DeviceType,
					&m.PM03, &m.PM03Raw, &m.PM01, &m.PM01Raw, &m.CO2, &m.CO2Raw,
					&m.TVOC, &m.TVOCRaw, &m.Nox, &m.NoxRaw,
					&m.TemperatureRaw, &m.HumidityRaw, &m.PressureRaw,
				); err == nil {
					res = append(res, m)
				}
			}
			rows.Close()
			if len(res) > 0 {
				return res
			}
		}
	}

	if hasJSON && jsonFile != "" {
		s.monitorFileMu.RLock()
		data, err := os.ReadFile(jsonFile)
		s.monitorFileMu.RUnlock()
		if err == nil {
			var all []monitor.Measurement
			if err := json.Unmarshal(data, &all); err == nil {
				var res []monitor.Measurement
				for _, m := range all {
					if m.DeviceID == deviceID && m.Timestamp.After(since) {
						res = append(res, m)
					}
				}
				if len(res) > 0 {
					return res
				}
			}
		}
	}
	return nil
}
