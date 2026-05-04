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
	fileMu      sync.RWMutex     // protects JSON file from concurrent writes
}

// SetNotifier attaches a Notifier that will receive warning callbacks.
func (s *MonitorService) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// LastMeasurement returns the most recent Measurement for the given deviceID,
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
		cfg:     cfg,
		history: make(map[string][]Measurement),
	}
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
	// 1. JSON Persistence (Always write to JSON if json_file is set)
	if s.cfg.Database.JsonFile != "" {
		s.saveToJSON(m)
	}

	// 2. SQL Persistence (Write to Postgres only if configured)
	if s.db != nil && s.cfg.Database.Type == "postgres" {
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
	s.history[m.DeviceID] = hist

	// Trim history
	s.trimHistoryInternal()

	// Save
	s.saveHistory(m)

	// Check warnings
	s.notify(&m)
}

func (s *MonitorService) calculateDiff(m *Measurement) {
	hist := s.history[m.DeviceID]
	if len(hist) == 0 {
		return
	}

	// Always compare with the previous measurement in history
	prev := &hist[len(hist)-1]
	actualDiffSec := m.Timestamp.Sub(prev.Timestamp).Seconds()

	m.DiffTime = int(actualDiffSec)
	m.PM10Prev = prev.PM10
	m.PM25Prev = prev.PM25

	if prev.PM10 > 0 {
		diff := ((m.PM10 - prev.PM10) / prev.PM10) * 100.0
		m.PM10Diff = &diff
	}
	if prev.PM25 > 0 {
		diff := ((m.PM25 - prev.PM25) / prev.PM25) * 100.0
		m.PM25Diff = &diff
	}
}

const (
	zoneGreen  = 0
	zoneYellow = 1
	zoneRed    = 2
)

func getZone(val, green, yellow float64) int {
	if val <= green {
		return zoneGreen
	}
	if val <= yellow {
		return zoneYellow
	}
	return zoneRed
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

		// Ensure PM10Yellow/PM25Yellow >= PM10Green/PM25Green
		pm10Green := mcfg.PM10Green
		pm10Yellow := mcfg.PM10Yellow
		if pm10Yellow < pm10Green {
			pm10Yellow = pm10Green
		}
		pm25Green := mcfg.PM25Green
		pm25Yellow := mcfg.PM25Yellow
		if pm25Yellow < pm25Green {
			pm25Yellow = pm25Green
		}

		z10 := getZone(m.PM10, pm10Green, pm10Yellow)
		z25 := getZone(m.PM25, pm25Green, pm25Yellow)
		prevZ10 := getZone(m.PM10Prev, pm10Green, pm10Yellow)
		prevZ25 := getZone(m.PM25Prev, pm25Green, pm25Yellow)

		var soundMessages []string
		var silentMessages []string
		var clearMessages []string // Used for "vals-gd" or similar "back to green"

		warnings := make(map[string]bool)
		for _, w := range mcfg.Warnings {
			warnings[w] = true
		}

		// Sound Notifications (val10-*, val25-*, vals-*)
		
		// PM10 transitions
		p10d := 0.0
		if m.PM10Diff != nil {
			p10d = *m.PM10Diff
		}

		if warnings["val10-yu"] && z10 == zoneYellow && prevZ10 == zoneGreen {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val10_yu", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
		}
		if warnings["val10-ru"] && z10 == zoneRed && prevZ10 != zoneRed {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val10_ru", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
		}
		if warnings["val10-yd"] && z10 == zoneYellow && prevZ10 == zoneRed {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val10_yd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
		}
		if warnings["val10-gd"] && z10 == zoneGreen && prevZ10 != zoneGreen {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val10_gd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
		}

		// PM2.5 transitions
		p25d := 0.0
		if m.PM25Diff != nil {
			p25d = *m.PM25Diff
		}

		if warnings["val25-yu"] && z25 == zoneYellow && prevZ25 == zoneGreen {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val25_yu", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}
		if warnings["val25-ru"] && z25 == zoneRed && prevZ25 != zoneRed {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val25_ru", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}
		if warnings["val25-yd"] && z25 == zoneYellow && prevZ25 == zoneRed {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val25_yd", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}
		if warnings["val25-gd"] && z25 == zoneGreen && prevZ25 != zoneGreen {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_val25_gd", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}

		// Combined transitions
		if warnings["vals-yu"] && (z10 >= zoneYellow && z25 >= zoneYellow) && (prevZ10 == zoneGreen && prevZ25 == zoneGreen) && (z10 == zoneYellow || z25 == zoneYellow) {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_vals_yu", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}
		if warnings["vals-ru"] && (z10 == zoneRed && z25 == zoneRed) && (prevZ10 != zoneRed || prevZ25 != zoneRed) {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_vals_ru", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}
		if warnings["vals-yd"] && (z10 == zoneYellow && z25 == zoneYellow) && (prevZ10 == zoneRed && prevZ25 == zoneRed) {
			soundMessages = append(soundMessages, s.notifier.T(chatID, "alert_vals_yd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}
		if warnings["vals-gd"] && (z10 == zoneGreen && z25 == zoneGreen) && (prevZ10 != zoneGreen || prevZ25 != zoneGreen) {
			clearMessages = append(clearMessages, s.notifier.T(chatID, "alert_vals_gd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
		}

		// Silent Notifications (diff10-*, diff25-*, diffs-*)
		pm10DiffExceeded := m.PM10Diff != nil && *m.PM10Diff >= mcfg.PM10Diff
		pm25DiffExceeded := m.PM25Diff != nil && *m.PM25Diff >= mcfg.PM25Diff
		pm10NegDiffExceeded := m.PM10Diff != nil && *m.PM10Diff <= -mcfg.PM10Diff
		pm25NegDiffExceeded := m.PM25Diff != nil && *m.PM25Diff <= -mcfg.PM25Diff

		// Growth
		if pm10DiffExceeded {
			if warnings["diff10-gu"] && z10 == zoneGreen {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_gu", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
			}
			if warnings["diff10-yu"] && z10 == zoneYellow {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_yu", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
			}
			if warnings["diff10-ru"] && z10 == zoneRed {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_ru", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
			}
		}
		if pm25DiffExceeded {
			if warnings["diff25-gu"] && z25 == zoneGreen {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_gu", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diff25-yu"] && z25 == zoneYellow {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_yu", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diff25-ru"] && z25 == zoneRed {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_ru", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
		}
		if pm10DiffExceeded && pm25DiffExceeded {
			if warnings["diffs-gu"] && z10 == zoneGreen && z25 == zoneGreen {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_gu", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diffs-yu"] && z10 == zoneYellow && z25 == zoneYellow {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_yu", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diffs-ru"] && z10 == zoneRed && z25 == zoneRed {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_ru", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
		}

		// Drop
		if pm10NegDiffExceeded {
			if warnings["diff10-gd"] && z10 == zoneGreen {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_gd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
			}
			if warnings["diff10-yd"] && z10 == zoneYellow {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_yd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
			}
			if warnings["diff10-rd"] && z10 == zoneRed {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff10_rd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow))
			}
		}
		if pm25NegDiffExceeded {
			if warnings["diff25-gd"] && z25 == zoneGreen {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_gd", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diff25-yd"] && z25 == zoneYellow {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_yd", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diff25-rd"] && z25 == zoneRed {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diff25_rd", m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
		}
		if pm10NegDiffExceeded && pm25NegDiffExceeded {
			if warnings["diffs-gd"] && z10 == zoneGreen && z25 == zoneGreen {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_gd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diffs-yd"] && z10 == zoneYellow && z25 == zoneYellow {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_yd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
			if warnings["diffs-rd"] && z10 == zoneRed && z25 == zoneRed {
				silentMessages = append(silentMessages, s.notifier.T(chatID, "alert_diffs_rd", m.PM10-m.PM10Prev, p10d, m.PM10Prev, m.PM10, pm10Green, pm10Yellow, m.PM25-m.PM25Prev, p25d, m.PM25Prev, m.PM25, pm25Green, pm25Yellow))
			}
		}

		var finalMessages []string
		isTransition := false
		if len(soundMessages) > 0 {
			finalMessages = soundMessages
			isTransition = true
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
			// It should be loud if there's a transition (soundMessages) or a clear message.
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

func (s *MonitorService) Close() {
	// Add cleanup if needed
}
