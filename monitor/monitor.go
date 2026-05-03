package monitor

import (
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

type Measurement struct {
	DeviceID    string    `json:"device_id"`
	Timestamp   time.Time `json:"timestamp"`
	PM10        float64   `json:"pm10"`
	PM25        float64   `json:"pm25"`
	Temperature float64   `json:"temperature"`
	Humidity    float64   `json:"humidity"`
	Pressure    float64   `json:"pressure"`
	PM10Diff    *float64  `json:"pm10_diff"`
	PM25Diff    *float64  `json:"pm25_diff"`
	PM10Prev    float64   `json:"pm10_prev"`
	PM25Prev    float64   `json:"pm25_prev"`
	DiffTime    int       `json:"diff_time"`
}

// Notifier is implemented by anything that can deliver warning messages
// (e.g. the Telegram bot). Using an interface keeps the monitor package
// decoupled from the tgbot package.
type Notifier interface {
	// GetSubscribers returns all chat IDs subscribed to deviceID.
	GetSubscribers(deviceID string) []int64
	// GetUserSettings returns the personalized monitor settings for a chat.
	GetUserSettings(chatID int64) *config.Monitor
	// SendWarning delivers a warning message to a specific subscriber.
	SendWarning(chatID int64, deviceID string, m *Measurement, messages []string, silent bool)
	// SendClear delivers a "values returned to normal" notification to a specific subscriber.
	SendClear(chatID int64, deviceID string, m *Measurement, messages []string)
	// Notify delivers a unified notification with appropriate styling.
	Notify(chatID int64, deviceID string, m *Measurement, alertMessages []string, clearMessages []string, silent bool)
	// T returns a localized string for a given key and arguments.
	T(chatID int64, key string, args ...interface{}) string
}

type MonitorService struct {
	cfg      *config.Config
	history  map[string][]Measurement
	mu       sync.RWMutex
	notifier Notifier
	db       *sql.DB
	// alertStates tracks which absolute-threshold alert keys are currently
	// active per user per device.
	// chatID -> deviceID -> alertKey -> bool
	alertStates map[int64]map[string]map[string]bool
	persistChan chan Measurement // queue for persistent storage
	done        chan struct{}    // signal worker to stop
	fileMu      sync.RWMutex     // protects JSON file from concurrent writes
}

// SetNotifier attaches a Notifier that will receive warning callbacks.
func (s *MonitorService) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// LastMeasurement returns the most recent Measurement for the given deviceID,
// or nil if no data is available.
func (s *MonitorService) LastMeasurement(deviceID string) *Measurement {
	s.mu.Lock()
	defer s.mu.Unlock()
	hist := s.history[deviceID]
	if len(hist) == 0 {
		return nil
	}
	copy := hist[len(hist)-1]
	return &copy
}

// GetMonitorConfig returns a copy of the current monitor configuration.
// Deprecated: use personalized settings from notifier.
func (s *MonitorService) GetMonitorConfig() config.Monitor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Monitor
}

// GetHistory returns a slice of recent measurements for the given deviceID.
func (s *MonitorService) GetHistory(deviceID string) []Measurement {
	s.mu.Lock()
	defer s.mu.Unlock()
	hist := s.history[deviceID]
	if len(hist) == 0 {
		return nil
	}
	// Return a copy to avoid race conditions
	res := make([]Measurement, len(hist))
	copy(res, hist)
	return res
}

func NewMonitorService(cfg *config.Config) *MonitorService {
	s := &MonitorService{
		cfg:         cfg,
		history:     make(map[string][]Measurement),
		alertStates: make(map[int64]map[string]map[string]bool),
	}
	s.persistChan = make(chan Measurement, 100)
	s.done = make(chan struct{})
	go s.persistenceWorker()
	s.loadHistory()
	return s
}

func (s *MonitorService) SetDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db

	// Initialize table
	query := `
	CREATE TABLE IF NOT EXISTS measurements (
		device_id TEXT,
		timestamp TIMESTAMPTZ,
		pm10 DOUBLE PRECISION,
		pm25 DOUBLE PRECISION,
		temperature DOUBLE PRECISION,
		humidity DOUBLE PRECISION,
		pressure DOUBLE PRECISION
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_measurements_unique ON measurements (device_id, timestamp);
	CREATE INDEX IF NOT EXISTS idx_measurements_device_time ON measurements (device_id, timestamp);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		log.Error().Err(err).Msg("failed to initialize SQL table")
		return
	}

	// Always attempt incremental migration from JSON to Postgres
	s.migrateFromJSON()

	// Load last N values into RAM for each device
	s.loadHistoryFromSQL()
}

// SyncDB attempts to reconcile JSON data with Postgres.
func (s *MonitorService) SyncDB() {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()

	if db == nil {
		return
	}
	if err := db.Ping(); err != nil {
		return
	}
	s.migrateFromJSON()
}

func (s *MonitorService) migrateFromJSON() {
	if s.cfg.Database.JsonFile == "" {
		return
	}
	data, err := os.ReadFile(s.cfg.Database.JsonFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Err(err).Msg("monitor: migration failed to read json")
		}
		return
	}

	var all []Measurement
	if err := json.Unmarshal(data, &all); err != nil {
		log.Error().Err(err).Msg("monitor: migration failed to unmarshal json")
		return
	}

	if len(all) == 0 {
		return
	}

	log.Info().Str("file", s.cfg.Database.JsonFile).Int("total_records", len(all)).Msg("monitor: ensuring json data is synced to postgres...")
	
	countMigrated := 0
	batchSize := 100
	for i := 0; i < len(all); i += batchSize {
		end := i + batchSize
		if end > len(all) {
			end = len(all)
		}
		batch := all[i:end]

		err := func() error {
			tx, err := s.db.Begin()
			if err != nil {
				return err
			}
			defer tx.Rollback()

			stmt, err := tx.Prepare(`
				INSERT INTO measurements (device_id, timestamp, pm10, pm25, temperature, humidity, pressure)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (device_id, timestamp) DO NOTHING
			`)
			if err != nil {
				return err
			}
			defer stmt.Close()

			for _, m := range batch {
				res, err := stmt.Exec(m.DeviceID, m.Timestamp, m.PM10, m.PM25, m.Temperature, m.Humidity, m.Pressure)
				if err == nil {
					n, _ := res.RowsAffected()
					if n > 0 {
						countMigrated++
					}
				}
			}
			return tx.Commit()
		}()

		if err != nil {
			log.Error().Err(err).Int("start", i).Msg("monitor: migration batch failed")
		}
	}

	if countMigrated > 0 {
		log.Info().Int("count", countMigrated).Msg("monitor: sync from json successful")
	} else {
		log.Debug().Msg("monitor: postgres is already up to date with json")
	}
}

// isAlertActive returns true if the given alert key is currently active for
// the user and device. Must be called with s.mu held.
func (s *MonitorService) isAlertActive(chatID int64, deviceID, key string) bool {
	if m1, ok := s.alertStates[chatID]; ok {
		if m2, ok := m1[deviceID]; ok {
			return m2[key]
		}
	}
	return false
}

// setAlertActive updates the alert state for key/device/user.
// Must be called with s.mu held.
func (s *MonitorService) setAlertActive(chatID int64, deviceID, key string, active bool) {
	if _, ok := s.alertStates[chatID]; !ok {
		s.alertStates[chatID] = make(map[string]map[string]bool)
	}
	if _, ok := s.alertStates[chatID][deviceID]; !ok {
		s.alertStates[chatID][deviceID] = make(map[string]bool)
	}
	s.alertStates[chatID][deviceID][key] = active
}

func (s *MonitorService) loadHistory() {
	if s.cfg.Database.Type != "json" || s.cfg.Database.JsonFile == "" {
		return
	}
	data, err := os.ReadFile(s.cfg.Database.JsonFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Err(err).Str("file", s.cfg.Database.JsonFile).Msg("failed to read history file")
		}
		return
	}
	var all []Measurement
	if err := json.Unmarshal(data, &all); err != nil {
		log.Error().Err(err).Str("file", s.cfg.Database.JsonFile).Msg("failed to unmarshal history")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range all {
		s.history[m.DeviceID] = append(s.history[m.DeviceID], m)
	}
	s.trimHistoryInternal()
	log.Info().Int("records", len(all)).Msg("monitor: history loaded from JSON")
}

func (s *MonitorService) loadHistoryFromSQL() {
	// Must be called with s.mu held if s.history is already being used,
	// but SetDB holds the lock.

	// 1. Get unique device IDs
	rows, err := s.db.Query("SELECT DISTINCT device_id FROM measurements")
	if err != nil {
		log.Error().Err(err).Msg("monitor: failed to query device IDs for history load")
		return
	}
	defer rows.Close()

	var deviceIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			deviceIDs = append(deviceIDs, id)
		}
	}

	max := s.cfg.System.ValuesInRam
	if max <= 0 {
		max = 10
	}

	for _, id := range deviceIDs {
		mRows, err := s.db.Query(`
			SELECT device_id, timestamp, pm10, pm25, temperature, humidity, pressure 
			FROM measurements 
			WHERE device_id = $1 
			ORDER BY timestamp DESC 
			LIMIT $2`, id, max)
		if err != nil {
			log.Error().Err(err).Str("device", id).Msg("monitor: failed to query history for device")
			continue
		}

		var deviceHist []Measurement
		for mRows.Next() {
			var m Measurement
			if err := mRows.Scan(&m.DeviceID, &m.Timestamp, &m.PM10, &m.PM25, &m.Temperature, &m.Humidity, &m.Pressure); err == nil {
				deviceHist = append([]Measurement{m}, deviceHist...) // Prepend to keep chronological order
			}
		}
		mRows.Close()
		s.history[id] = deviceHist
	}
	log.Info().Int("devices", len(deviceIDs)).Int("limit_per_device", max).Msg("monitor: history loaded from SQL")
}

func (s *MonitorService) trimHistoryInternal() {
	max := s.cfg.System.ValuesInRam
	if max <= 0 {
		return
	}
	for deviceID, hist := range s.history {
		if len(hist) > max {
			s.history[deviceID] = hist[len(hist)-max:]
		}
	}
}

func (s *MonitorService) saveHistory(m Measurement) {
	// 1. JSON Persistence
	if s.cfg.Database.Type == "json" && s.cfg.Database.JsonFile != "" {
		s.saveToJSON(m)
	}

	// 2. SQL Persistence
	if s.db != nil {
		s.saveToSQL(m)
	}
}

func (s *MonitorService) saveToJSON(m Measurement) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	data, err := os.ReadFile(s.cfg.Database.JsonFile)
	var all []Measurement
	if err == nil {
		_ = json.Unmarshal(data, &all)
	}
	all = append(all, m)

	// Optional: keep total file size reasonable (max_values from config)
	if s.cfg.Database.MaxValues > 0 && len(all) > s.cfg.Database.MaxValues {
		all = all[len(all)-s.cfg.Database.MaxValues:]
	}

	out, err := json.MarshalIndent(all, "", "  ")
	if err == nil {
		_ = os.WriteFile(s.cfg.Database.JsonFile, out, 0644)
	}
}

func (s *MonitorService) saveToSQL(m Measurement) {
	log.Debug().Str("device", m.DeviceID).Time("ts", m.Timestamp).Msg("db: saving measurement to sql")
	query := "INSERT INTO measurements (device_id, timestamp, pm10, pm25, temperature, humidity, pressure) VALUES ($1, $2, $3, $4, $5, $6, $7)"
	_, err := s.db.Exec(query, m.DeviceID, m.Timestamp, m.PM10, m.PM25, m.Temperature, m.Humidity, m.Pressure)
	if err != nil {
		log.Error().Err(err).Msg("failed to save to SQL")
	}
}

func (s *MonitorService) Process(data *sensor.SensorData) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m := Measurement{
		DeviceID:  data.ParentID,
		Timestamp: data.DateTime,
	}

	for _, v := range data.Values {
		val, err := strconv.ParseFloat(v.Value, 64)
		if err != nil {
			log.Warn().Err(err).Str("device", data.ParentID).Str("type", v.Type).Str("value", v.Value).Msg("failed to parse sensor value")
			continue
		}
		switch v.Type {
		case "SDS_P1":
			m.PM10 = val
		case "SDS_P2":
			m.PM25 = val
		case "BME280_temperature":
			m.Temperature = val
		case "BME280_humidity":
			m.Humidity = val
		case "BME280_pressure":
			m.Pressure = val / 100.0
		}
	}

	// Calculate diff BEFORE adding to history
	s.calculateDiff(&m)

	// Add to history
	hist := s.history[m.DeviceID]
	hist = append(hist, m)

	// Trim history
	max := s.cfg.System.ValuesInRam
	if max > 0 && len(hist) > max {
		hist = hist[len(hist)-max:]
	}
	s.history[m.DeviceID] = hist

	// Save to JSON
	s.saveHistory(m)

	// Check warnings
	s.notify(&m)
}

func (s *MonitorService) calculateDiff(m *Measurement) {
	hist := s.history[m.DeviceID]
	if len(hist) == 0 {
		return
	}

	diffLimit := float64(s.cfg.Monitor.DiffTime)
	var bestMatch *Measurement
	var maxDiff float64 = -1

	for i := 0; i < len(hist); i++ {
		prev := &hist[i]
		actualDiffSec := m.Timestamp.Sub(prev.Timestamp).Seconds()
		// Get readings closest in time to the diffLimit
		if actualDiffSec > 0 && actualDiffSec <= diffLimit+10 {
			if actualDiffSec > maxDiff {
				maxDiff = actualDiffSec
				bestMatch = prev
			}
		}
	}

	if bestMatch != nil {
		m.DiffTime = int(maxDiff)
		m.PM10Prev = bestMatch.PM10
		m.PM25Prev = bestMatch.PM25

		if bestMatch.PM10 > 0 {
			diff := ((m.PM10 - bestMatch.PM10) / bestMatch.PM10) * 100.0
			m.PM10Diff = &diff
		}
		if bestMatch.PM25 > 0 {
			diff := ((m.PM25 - bestMatch.PM25) / bestMatch.PM25) * 100.0
			m.PM25Diff = &diff
		}
	}
}

func (s *MonitorService) notify(m *Measurement) {
	if s.notifier == nil {
		return
	}

	subscribers := s.notifier.GetSubscribers(m.DeviceID)
	if len(subscribers) == 0 {
		return
	}

	for _, chatID := range subscribers {
		mcfg := s.notifier.GetUserSettings(chatID)
		if mcfg == nil {
			continue
		}

		var soundMessages []string
		var silentMessages []string
		var clearMessages []string
		isTransition := false

		warnings := make(map[string]bool)
		for _, w := range mcfg.Warnings {
			warnings[w] = true
		}

		pm10AbsExceeded := m.PM10 >= mcfg.PM10Value
		pm25AbsExceeded := m.PM25 >= mcfg.PM25Value

		// val10, val25, vals — edge-triggered: fire only on state change.
		// When vals is enabled it can be included alongside val10/val25.

		// val10: absolute PM10 threshold. Triggers when exceeding or returning below the threshold.
		if warnings["val10"] {
			wasActive := s.isAlertActive(chatID, m.DeviceID, "val10")
			if pm10AbsExceeded && !wasActive {
				s.setAlertActive(chatID, m.DeviceID, "val10", true)
				isTransition = true
				soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val10_exceeded",
					m.PM10, mcfg.PM10Value))
			} else if !pm10AbsExceeded && wasActive {
				s.setAlertActive(chatID, m.DeviceID, "val10", false)
				clearMessages = append(clearMessages, s.notifier.T(chatID, "alert_val10_normal", m.PM10, mcfg.PM10Value))
			}
		}
		// val25: absolute PM2.5 threshold. Triggers when exceeding or returning below the threshold.
		if warnings["val25"] {
			wasActive := s.isAlertActive(chatID, m.DeviceID, "val25")
			if pm25AbsExceeded && !wasActive {
				s.setAlertActive(chatID, m.DeviceID, "val25", true)
				isTransition = true
				soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val25_exceeded",
					m.PM25, mcfg.PM25Value))
			} else if !pm25AbsExceeded && wasActive {
				s.setAlertActive(chatID, m.DeviceID, "val25", false)
				clearMessages = append(clearMessages, s.notifier.T(chatID, "alert_val25_normal", m.PM25, mcfg.PM25Value))
			}
		}
		// vals: triggers when both PM10 and PM2.5 absolute thresholds are exceeded.
		if warnings["vals"] {
			bothExceeded := pm10AbsExceeded && pm25AbsExceeded
			wasActive := s.isAlertActive(chatID, m.DeviceID, "vals")
			if bothExceeded && !wasActive {
				s.setAlertActive(chatID, m.DeviceID, "vals", true)
				isTransition = true
				soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_vals_exceeded",
					m.PM10, m.PM25))
			} else if !bothExceeded && wasActive {
				s.setAlertActive(chatID, m.DeviceID, "vals", false)
				clearMessages = append(clearMessages, s.notifier.T(chatID, "alert_vals_normal",
					m.PM10, m.PM25))
			}
		}

		// diff10, diff25, diffs
		pm10DiffExceeded := m.PM10Diff != nil && *m.PM10Diff >= mcfg.PM10Diff
		pm25DiffExceeded := m.PM25Diff != nil && *m.PM25Diff >= mcfg.PM25Diff

		// diff10: PM10 percentage growth >= pm10_diff
		if warnings["diff10"] && pm10DiffExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_growth",
				*m.PM10Diff, mcfg.PM10Diff, m.DiffTime, m.PM10Prev, m.PM10))
		}
		// diff25: PM2.5 percentage growth >= pm25_diff
		if warnings["diff25"] && pm25DiffExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_growth",
				*m.PM25Diff, mcfg.PM25Diff, m.DiffTime, m.PM25Prev, m.PM25))
		}
		// diffs: both PM10 and PM2.5 increase simultaneously by their respective diff values
		if warnings["diffs"] && pm10DiffExceeded && pm25DiffExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_growth",
				*m.PM10Diff, *m.PM25Diff, m.DiffTime))
		}

		// diff10_neg, diff25_neg, diffs_neg
		pm10NegDiffExceeded := m.PM10Diff != nil && *m.PM10Diff <= -mcfg.PM10Diff
		pm25NegDiffExceeded := m.PM25Diff != nil && *m.PM25Diff <= -mcfg.PM25Diff

		// diff10_neg: PM10 percentage decrease >= pm10_diff
		if warnings["diff10_neg"] && pm10NegDiffExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_decrease",
				*m.PM10Diff, m.DiffTime, m.PM10Prev, m.PM10))
		}
		// diff25_neg: PM2.5 percentage decrease >= pm25_diff
		if warnings["diff25_neg"] && pm25NegDiffExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_decrease",
				*m.PM25Diff, m.DiffTime, m.PM25Prev, m.PM25))
		}
		// diffs_neg: both PM10 and PM2.5 decrease simultaneously by their respective diff values
		if warnings["diffs_neg"] && pm10NegDiffExceeded && pm25NegDiffExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_decrease",
				*m.PM10Diff, *m.PM25Diff, m.DiffTime))
		}

		// diff10_over, diff25_over, diffs_over
		// diff10_over: PM10 growth >= pm10_diff while already above absolute threshold
		if warnings["diff10_over"] && pm10DiffExceeded && pm10AbsExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_crit",
				*m.PM10Diff, m.DiffTime, m.PM10))
		}
		// diff25_over: PM2.5 growth >= pm25_diff while already above absolute threshold
		if warnings["diff25_over"] && pm25DiffExceeded && pm25AbsExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_crit",
				*m.PM25Diff, m.DiffTime, m.PM25))
		}
		// diffs_over: both PM10/PM2.5 growth >= diff values while both are above absolute thresholds
		if warnings["diffs_over"] && pm10DiffExceeded && pm25DiffExceeded && pm10AbsExceeded && pm25AbsExceeded {
			silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_crit",
				*m.PM10Diff, *m.PM25Diff))
		}

		// diff10_neg_over, diff25_neg_over, diffs_neg_over
		// diff10_neg_over: PM10 decrease >= pm10_diff resulting in value below absolute threshold
		if warnings["diff10_neg_over"] && pm10NegDiffExceeded && !pm10AbsExceeded {
			isTransition = true
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_diff10_clean",
				*m.PM10Diff, m.PM10))
		}
		// diff25_neg_over: PM2.5 decrease >= pm25_diff resulting in value below absolute threshold
		if warnings["diff25_neg_over"] && pm25NegDiffExceeded && !pm25AbsExceeded {
			isTransition = true
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_diff25_clean",
				*m.PM25Diff, m.PM25))
		}
		// diffs_neg_over: both PM10/PM2.5 decrease >= diff values resulting in both values below thresholds
		if warnings["diffs_neg_over"] && pm10NegDiffExceeded && pm25NegDiffExceeded && !pm10AbsExceeded && !pm25AbsExceeded {
			isTransition = true
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_diffs_clean",
				*m.PM10Diff, *m.PM25Diff))
		}

		var finalMessages []string
		if len(soundMessages) > 0 || len(clearMessages) > 0 {
			finalMessages = soundMessages
		} else {
			finalMessages = silentMessages
		}

		if len(finalMessages) > 0 || len(clearMessages) > 0 {
			for _, msg := range finalMessages {
				log.Warn().Int64("chat", chatID).Str("device", m.DeviceID).Msg(msg)
			}
			for _, msg := range clearMessages {
				log.Info().Int64("chat", chatID).Str("device", m.DeviceID).Msg("cleared: " + msg)
			}
			// Use the unified Notify method.
			// It should be loud if there's a transition or a clear message.
			s.notifier.Notify(chatID, m.DeviceID, m, finalMessages, clearMessages, !isTransition && len(clearMessages) == 0)
		}
	}
}

// GetHistoryByDuration returns history for the given device up to 24h back.
// It prioritizes DB/Persistent store if available.
func (s *MonitorService) GetHistoryByDuration(deviceID string, duration time.Duration) []Measurement {
	var res []Measurement
	// 1. Try SQL
	if s.db != nil {
		res = s.getHistoryFromSQL(deviceID, duration)
		if len(res) > 0 {
			return res
		}
	}
	// 2. Try JSON
	if s.cfg.Database.JsonFile != "" {
		res = s.getHistoryFromJSON(deviceID, duration)
		if len(res) > 0 {
			return res
		}
	}
	// 3. Try RAM
	res = s.GetHistory(deviceID)
	log.Debug().Str("device", deviceID).Dur("duration", duration).Int("count", len(res)).Msg("monitor: history fetched (fallback)")
	return res
}

func (s *MonitorService) getHistoryFromSQL(deviceID string, duration time.Duration) []Measurement {
	since := time.Now().UTC().Add(-duration)
	log.Debug().Str("device", deviceID).Dur("duration", duration).Msg("db: querying sql history")
	query := "SELECT device_id, timestamp, pm10, pm25, temperature, humidity, pressure FROM measurements WHERE device_id = $1 AND timestamp >= $2 ORDER BY timestamp ASC"
	rows, err := s.db.Query(query, deviceID, since)
	if err != nil {
		log.Error().Err(err).Msg("failed to query SQL history")
		return nil
	}
	defer rows.Close()

	var res []Measurement
	for rows.Next() {
		var m Measurement
		if err := rows.Scan(&m.DeviceID, &m.Timestamp, &m.PM10, &m.PM25, &m.Temperature, &m.Humidity, &m.Pressure); err == nil {
			res = append(res, m)
		}
	}
	return res
}

func (s *MonitorService) getHistoryFromJSON(deviceID string, duration time.Duration) []Measurement {
	s.fileMu.RLock()
	defer s.fileMu.RUnlock()

	data, err := os.ReadFile(s.cfg.Database.JsonFile)
	if err != nil {
		return nil
	}
	var all []Measurement
	json.Unmarshal(data, &all)

	since := time.Now().UTC().Add(-duration)
	var res []Measurement
	for _, m := range all {
		if m.DeviceID == deviceID && m.Timestamp.After(since) {
			res = append(res, m)
		}
	}
	return res
}

func (s *MonitorService) persistenceWorker() {
	for {
		select {
		case m := <-s.persistChan:
			s.saveHistory(m)
		case <-s.done:
			// Drain channel before exiting
			close(s.persistChan)
			for m := range s.persistChan {
				s.saveHistory(m)
			}
			return
		}
	}
}

func (s *MonitorService) Close() {
	close(s.done)
}
