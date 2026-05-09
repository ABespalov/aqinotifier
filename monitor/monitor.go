package monitor

import (
	"database/sql"
	"encoding/json"
	"math"
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

// AlertEvent represents a specific monitoring event that triggered a notification.
// It carries the event type and optional associated value (like AQI).
type AlertEvent struct {
	ID    string
	Value float64
}

// Notifier is implemented by anything that can deliver warning messages
// (e.g. the Telegram bot). Using an interface keeps the monitor package
// decoupled from the tgbot package.
type Notifier interface {
	// GetSubscribers returns all chat IDs subscribed to deviceID.
	GetSubscribers(deviceID string) []int64
	// GetUserSettings returns the personalized monitor settings for a chat.
	GetUserSettings(chatID int64) *config.Monitor
	// Notify delivers a unified notification with appropriate styling based on events.
	Notify(chatID int64, m *Measurement, alerts []AlertEvent, clears []AlertEvent, silent bool)
}

type MonitorService struct {
	cfg      *config.Config
	history  map[string][]Measurement
	mu       sync.RWMutex
	notifier Notifier
	db       *sql.DB
	fileMu   sync.RWMutex // protects JSON file from concurrent writes
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

	// Load last N values into RAM for each device
	s.loadHistoryFromSQL()
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
	for id := range s.history {
		s.recalculateDiffs(id)
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
		s.recalculateDiffs(id)
	}
	log.Info().Int("devices", len(deviceIDs)).Int("limit_per_device", max).Msg("monitor: history loaded from SQL")
}

func (s *MonitorService) recalculateDiffs(deviceID string) {
	hist := s.history[deviceID]
	if len(hist) < 2 {
		return
	}
	for i := 1; i < len(hist); i++ {
		prev := &hist[i-1]
		curr := &hist[i]

		curr.PM10Prev = prev.PM10
		curr.PM25Prev = prev.PM25
		curr.DiffTime = int(curr.Timestamp.Sub(prev.Timestamp).Seconds())

		if prev.PM10 > 0 {
			diff := ((curr.PM10 - prev.PM10) / prev.PM10) * 100.0
			curr.PM10Diff = &diff
		}
		if prev.PM25 > 0 {
			diff := ((curr.PM25 - prev.PM25) / prev.PM25) * 100.0
			curr.PM25Diff = &diff
		}
	}
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

		notifications := make(map[string]bool)
		for _, n := range mcfg.Notifications {
			notifications[n] = true
		}

		warnings := make(map[string]bool)
		for _, w := range mcfg.Warnings {
			warnings[w] = true
		}

		p10d := 0.0
		if m.PM10Diff != nil {
			p10d = *m.PM10Diff
		}
		p25d := 0.0
		if m.PM25Diff != nil {
			p25d = *m.PM25Diff
		}

		var soundEvents []AlertEvent
		var silentEvents []AlertEvent
		var clearEvents []AlertEvent
		isLoud := false
		addEvent := func(id string, val ...float64) {
			if !notifications[id] {
				return
			}
			v := 0.0
			if len(val) > 0 {
				v = val[0]
			}
			evt := AlertEvent{ID: id, Value: v}
			if warnings[id] {
				soundEvents = append(soundEvents, evt)
				isLoud = true
			} else {
				silentEvents = append(silentEvents, evt)
			}
		}

		addClear := func(id string, val ...float64) {
			if !notifications[id] {
				return
			}
			v := 0.0
			if len(val) > 0 {
				v = val[0]
			}
			evt := AlertEvent{ID: id, Value: v}
			clearEvents = append(clearEvents, evt)
			if warnings[id] {
				isLoud = true
			}
		}

		// PM10 transitions
		if z10 == zoneYellow && prevZ10 == zoneGreen {
			addEvent("val10-yu")
		}
		if z10 == zoneRed && prevZ10 != zoneRed {
			addEvent("val10-ru")
		}
		if z10 == zoneYellow && prevZ10 == zoneRed {
			addEvent("val10-yd")
		}
		if z10 == zoneGreen && prevZ10 != zoneGreen {
			addClear("val10-gd")
		}

		// PM2.5 transitions
		if z25 == zoneYellow && prevZ25 == zoneGreen {
			addEvent("val25-yu")
		}
		if z25 == zoneRed && prevZ25 != zoneRed {
			addEvent("val25-ru")
		}
		if z25 == zoneYellow && prevZ25 == zoneRed {
			addEvent("val25-yd")
		}
		if z25 == zoneGreen && prevZ25 != zoneGreen {
			addClear("val25-gd")
		}

		// Combined transitions
		if (z10 >= zoneYellow && z25 >= zoneYellow) && (prevZ10 == zoneGreen && prevZ25 == zoneGreen) && (z10 == zoneYellow || z25 == zoneYellow) {
			addEvent("vals-yu")
		}
		if (z10 == zoneRed && z25 == zoneRed) && (prevZ10 != zoneRed || prevZ25 != zoneRed) {
			addEvent("vals-ru")
		}
		if (z10 == zoneYellow && z25 == zoneYellow) && (prevZ10 == zoneRed && prevZ25 == zoneRed) {
			addEvent("vals-yd")
		}
		if (z10 == zoneGreen && z25 == zoneGreen) && (prevZ10 != zoneGreen || prevZ25 != zoneGreen) {
			addClear("vals-gd")
		}

		// AQI Notifications
		var aqi float64
		var level sensor.AQILevel
		var prevLevel sensor.AQILevel

		if mcfg.AQIStandard == "US" {
			aqi, level = sensor.CalculateUS_AQI(m.PM25, m.PM10)
			_, prevLevel = sensor.CalculateUS_AQI(m.PM25Prev, m.PM10Prev)
		} else {
			aqi, level = sensor.CalculateEU_AQI(m.PM25, m.PM10)
			_, prevLevel = sensor.CalculateEU_AQI(m.PM25Prev, m.PM10Prev)
		}

		if level != prevLevel {
			var aqiID string
			switch level {
			case sensor.LevelGood:
				aqiID = "aqi_z1"
			case sensor.LevelModerate:
				aqiID = "aqi_z2"
			case sensor.LevelSlightlyUnhealthy:
				aqiID = "aqi_z3"
			case sensor.LevelUnhealthy:
				aqiID = "aqi_z4"
			case sensor.LevelVeryUnhealthy:
				aqiID = "aqi_z5"
			case sensor.LevelHazardous:
				aqiID = "aqi_z6"
			case sensor.LevelExtremelyHazardous:
				aqiID = "aqi_z7"
			}

			if aqiID != "" {
				if level == sensor.LevelGood {
					addClear(aqiID, aqi)
				} else {
					addEvent(aqiID, aqi)
				}
			}
		}

		// Silent Notifications (Growth/Drop within zones)
		pm10DiffExceeded := m.PM10Diff != nil && math.Abs(*m.PM10Diff) >= mcfg.PM10Diff
		pm25DiffExceeded := m.PM25Diff != nil && math.Abs(*m.PM25Diff) >= mcfg.PM25Diff

		// PM10 Growth
		if pm10DiffExceeded && p10d > 0 {
			if z10 == zoneGreen {
				addEvent("diff10-gu")
			}
			if z10 == zoneYellow {
				addEvent("diff10-yu")
			}
			if z10 == zoneRed {
				addEvent("diff10-ru")
			}
		}
		// PM2.5 Growth
		if pm25DiffExceeded && p25d > 0 {
			if z25 == zoneGreen {
				addEvent("diff25-gu")
			}
			if z25 == zoneYellow {
				addEvent("diff25-yu")
			}
			if z25 == zoneRed {
				addEvent("diff25-ru")
			}
		}
		// Combined Growth
		if pm10DiffExceeded && p10d > 0 && pm25DiffExceeded && p25d > 0 {
			if z10 == zoneGreen && z25 == zoneGreen {
				addEvent("diffs-gu")
			}
			if z10 == zoneYellow && z25 == zoneYellow {
				addEvent("diffs-yu")
			}
			if z10 == zoneRed && z25 == zoneRed {
				addEvent("diffs-ru")
			}
		}
		// PM10 Drop
		if pm10DiffExceeded && p10d < 0 {
			if z10 == zoneGreen {
				addEvent("diff10-gd")
			}
			if z10 == zoneYellow {
				addEvent("diff10-yd")
			}
			if z10 == zoneRed {
				addEvent("diff10-rd")
			}
		}
		// PM2.5 Drop
		if pm25DiffExceeded && p25d < 0 {
			if z25 == zoneGreen {
				addEvent("diff25-gd")
			}
			if z25 == zoneYellow {
				addEvent("diff25-yd")
			}
			if z25 == zoneRed {
				addEvent("diff25-rd")
			}
		}
		// Combined Drop
		if pm10DiffExceeded && p10d < 0 && pm25DiffExceeded && p25d < 0 {
			if z10 == zoneGreen && z25 == zoneGreen {
				addEvent("diffs-gd")
			}
			if z10 == zoneYellow && z25 == zoneYellow {
				addEvent("diffs-yd")
			}
			if z10 == zoneRed && z25 == zoneRed {
				addEvent("diffs-rd")
			}
		}

		var finalEvents []AlertEvent
		finalEvents = append(finalEvents, soundEvents...)
		finalEvents = append(finalEvents, silentEvents...)

		if len(finalEvents) > 0 || len(clearEvents) > 0 {
			for _, evt := range finalEvents {
				log.Warn().Int64("chat", chatID).Str("device", m.DeviceID).Str("event", evt.ID).Msg("Alert triggered")
			}
			for _, evt := range clearEvents {
				log.Info().Int64("chat", chatID).Str("device", m.DeviceID).Str("event", evt.ID).Msg("Normal state restored")
			}
			// Use the unified Notify method.
			s.notifier.Notify(chatID, m, finalEvents, clearEvents, !isLoud)
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
