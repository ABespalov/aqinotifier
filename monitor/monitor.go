package monitor

import (
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

type Measurement struct {
	DeviceID       string    `json:"device_id"`
	Timestamp      time.Time `json:"timestamp"`
	PM10           float64   `json:"pm10"`
	PM25           float64   `json:"pm25"`
	PM10Raw        float64   `json:"pm10_raw"`
	PM25Raw        float64   `json:"pm25_raw"`
	Temperature    float64   `json:"temperature"`
	Humidity       float64   `json:"humidity"`
	Pressure       float64   `json:"pressure"`
	PM10Diff       *float64  `json:"pm10_diff"`
	PM25Diff       *float64  `json:"pm25_diff"`
	PM10Prev       float64   `json:"pm10_prev"`
	PM25Prev       float64   `json:"pm25_prev"`
	DiffTime       int       `json:"diff_time"`
	DeviceType     string    `json:"device_type"`
	PM03           float64   `json:"pm03"`
	PM03Raw        float64   `json:"pm03_raw"`
	PM01           float64   `json:"pm01"`
	PM01Raw        float64   `json:"pm01_raw"`
	CO2            float64   `json:"co2"`
	CO2Raw         float64   `json:"co2_raw"`
	TVOC           float64   `json:"tvoc"`
	TVOCRaw        float64   `json:"tvoc_raw"`
	Nox            float64   `json:"nox"`
	NoxRaw         float64   `json:"nox_raw"`
	TemperatureRaw float64   `json:"temperature_raw"`
	HumidityRaw    float64   `json:"humidity_raw"`
	PressureRaw    float64   `json:"pressure_raw"`
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
	// GetDeviceType returns the device type string for a device.
	GetDeviceType(deviceID string) string
}

type MonitorService struct {
	cfg        *config.Config
	history    map[string][]Measurement
	mu         sync.RWMutex
	notifier   Notifier
	db         *sql.DB
	fileMu     sync.RWMutex // protects JSON file from concurrent writes
	evaluators map[string]*DeviceEvaluator
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
	evaluatorsMap := make(map[string]*DeviceEvaluator)
	for devPrefix, corr := range cfg.Monitor.Corrections {
		eval, err := buildEvaluator(corr)
		if err != nil {
			log.Fatal().Err(err).Str("device_prefix", devPrefix).Msg("Failed to build formula evaluator")
		}
		evaluatorsMap[devPrefix] = eval
	}

	s := &MonitorService{
		cfg:        cfg,
		history:    make(map[string][]Measurement),
		evaluators: evaluatorsMap,
	}
	s.loadHistory()
	return s
}

// Reload rebuilds the formula evaluators when config changes.
func (s *MonitorService) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	evaluatorsMap := make(map[string]*DeviceEvaluator)
	for devPrefix, corr := range s.cfg.Monitor.Corrections {
		eval, err := buildEvaluator(corr)
		if err != nil {
			log.Error().Err(err).Str("device_prefix", devPrefix).Msg("Failed to build formula evaluator on reload")
			continue
		}
		evaluatorsMap[devPrefix] = eval
	}
	s.evaluators = evaluatorsMap
}

func (s *MonitorService) SetDB(db *sql.DB) {
	s.mu.Lock()
	s.db = db
	s.mu.Unlock()

	// Initialize table
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
		log.Error().Err(err).Msg("failed to initialize SQL table")
		return
	}

	// Migrate existing table by adding new columns if they do not exist
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

	// Backfill raw values for historical data (if raw is 0 but computed is not)
	_, _ = s.db.Exec("UPDATE measurements SET pm10_raw = pm10 WHERE pm10_raw = 0 AND pm10 != 0")
	_, _ = s.db.Exec("UPDATE measurements SET pm25_raw = pm25 WHERE pm25_raw = 0 AND pm25 != 0")

	// Load last N values into RAM for each device
	s.mu.Lock()
	s.loadHistoryFromSQLLocked()
	s.mu.Unlock()
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

	// Backfill raw values and device types for historical data
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

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range all {
		s.history[m.DeviceID] = append(s.history[m.DeviceID], m)
	}
	for id := range s.history {
		s.recalculateDiffsLocked(id)
	}
	s.trimHistoryInternal()
	log.Info().Int("records", len(all)).Msg("monitor: history loaded from JSON")
}

func (s *MonitorService) loadHistoryFromSQLLocked() {
	if s.db == nil {
		return
	}

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
			SELECT device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw, temperature, humidity, pressure, COALESCE(device_type, 'ArmAQI'),
			       pm03, pm03_raw, pm01, pm01_raw, co2, co2_raw, tvoc, tvoc_raw, nox, nox_raw, temperature_raw, humidity_raw, pressure_raw
			FROM measurements 
			WHERE device_id = $1 
			ORDER BY timestamp DESC 
			LIMIT $2`, id, max)
		if err != nil {
			log.Error().Err(err).Str("device", id).Msg("monitor: failed to query history for device")
			continue
		}

		var sqlHist []Measurement
		for mRows.Next() {
			var m Measurement
			if err := mRows.Scan(
				&m.DeviceID, &m.Timestamp, &m.PM10, &m.PM25, &m.PM10Raw, &m.PM25Raw, &m.Temperature, &m.Humidity, &m.Pressure, &m.DeviceType,
				&m.PM03, &m.PM03Raw, &m.PM01, &m.PM01Raw, &m.CO2, &m.CO2Raw, &m.TVOC, &m.TVOCRaw, &m.Nox, &m.NoxRaw, &m.TemperatureRaw, &m.HumidityRaw, &m.PressureRaw,
			); err == nil {
				sqlHist = append([]Measurement{m}, sqlHist...)
			}
		}
		mRows.Close()

		// Merge with existing (JSON) history
		existing := s.history[id]
		merged := append(existing, sqlHist...)

		// Deduplicate and sort
		unique := make(map[int64]Measurement)
		for _, m := range merged {
			unique[m.Timestamp.Unix()] = m
		}

		var final []Measurement
		for _, m := range unique {
			final = append(final, m)
		}
		sort.Slice(final, func(i, j int) bool {
			return final[i].Timestamp.Before(final[j].Timestamp)
		})

		if len(final) > max {
			final = final[len(final)-max:]
		}
		s.history[id] = final
		s.recalculateDiffsLocked(id)
	}
	log.Info().Int("devices", len(deviceIDs)).Int("limit_per_device", max).Msg("monitor: history loaded and merged from SQL")
}

func (s *MonitorService) recalculateDiffsLocked(deviceID string) {
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

	// Backfill old records to heal the JSON file
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
	go func(m Measurement) {
		log.Debug().Str("device", m.DeviceID).Time("ts", m.Timestamp).Msg("db: saving measurement to sql")
		query := `
			INSERT INTO measurements (
				device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw, temperature, humidity, pressure, device_type,
				pm03, pm03_raw, pm01, pm01_raw, co2, co2_raw, tvoc, tvoc_raw, nox, nox_raw, temperature_raw, humidity_raw, pressure_raw
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`
		_, err := s.db.Exec(query,
			m.DeviceID, m.Timestamp, m.PM10, m.PM25, m.PM10Raw, m.PM25Raw, m.Temperature, m.Humidity, m.Pressure, m.DeviceType,
			m.PM03, m.PM03Raw, m.PM01, m.PM01Raw, m.CO2, m.CO2Raw, m.TVOC, m.TVOCRaw, m.Nox, m.NoxRaw, m.TemperatureRaw, m.HumidityRaw, m.PressureRaw,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to save to SQL")
		}
	}(m)
}

func (s *MonitorService) Process(data *sensor.SensorData) {
	if data.DeviceType == "AirGradient" {
		s.processAirGradient(data)
		return
	}
	s.processArmAQI(data)
}



func (s *MonitorService) lastMeasurementLocked(deviceID string) *Measurement {
	hist := s.history[deviceID]
	if len(hist) == 0 {
		return nil
	}
	copy := hist[len(hist)-1]
	return &copy
}

func (s *MonitorService) calculateDiffLocked(m *Measurement) {
	hist := s.history[m.DeviceID]
	if len(hist) == 0 {
		return
	}

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

		// Ensure Level2 >= Level1
		pm10Level1 := mcfg.PM10L1
		pm10Level2 := mcfg.PM10L2
		if pm10Level2 < pm10Level1 {
			pm10Level2 = pm10Level1
		}
		pm25Level1 := mcfg.PM25L1
		pm25Level2 := mcfg.PM25L2
		if pm25Level2 < pm25Level1 {
			pm25Level2 = pm25Level1
		}

		z10 := getZone(m.PM10, pm10Level1, pm10Level2)
		z25 := getZone(m.PM25, pm25Level1, pm25Level2)
		prevZ10 := getZone(m.PM10Prev, pm10Level1, pm10Level2)
		prevZ25 := getZone(m.PM25Prev, pm25Level1, pm25Level2)

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
			addEvent("val10_l2u")
		}
		if z10 == zoneRed && prevZ10 != zoneRed {
			addEvent("val10_l3u")
		}
		if z10 == zoneYellow && prevZ10 == zoneRed {
			addEvent("val10_l2d")
		}
		if z10 == zoneGreen && prevZ10 != zoneGreen {
			addClear("val10_l1d")
		}

		// PM2.5 transitions
		if z25 == zoneYellow && prevZ25 == zoneGreen {
			addEvent("val25_l2u")
		}
		if z25 == zoneRed && prevZ25 != zoneRed {
			addEvent("val25_l3u")
		}
		if z25 == zoneYellow && prevZ25 == zoneRed {
			addEvent("val25_l2d")
		}
		if z25 == zoneGreen && prevZ25 != zoneGreen {
			addClear("val25_l1d")
		}

		// Combined transitions
		if (z10 >= zoneYellow && z25 >= zoneYellow) && (prevZ10 == zoneGreen && prevZ25 == zoneGreen) && (z10 == zoneYellow || z25 == zoneYellow) {
			addEvent("vals_l2u")
		}
		if (z10 == zoneRed && z25 == zoneRed) && (prevZ10 != zoneRed || prevZ25 != zoneRed) {
			addEvent("vals_l3u")
		}
		if (z10 == zoneYellow && z25 == zoneYellow) && (prevZ10 == zoneRed && prevZ25 == zoneRed) {
			addEvent("vals_l2d")
		}
		if (z10 == zoneGreen && z25 == zoneGreen) && (prevZ10 != zoneGreen || prevZ25 != zoneGreen) {
			addClear("vals_l1d")
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
				aqiID = "aqi_l1"
			case sensor.LevelModerate:
				aqiID = "aqi_l2"
			case sensor.LevelSlightlyUnhealthy:
				aqiID = "aqi_l3"
			case sensor.LevelUnhealthy:
				aqiID = "aqi_l4"
			case sensor.LevelVeryUnhealthy:
				aqiID = "aqi_l5"
			case sensor.LevelHazardous:
				aqiID = "aqi_l6"
			case sensor.LevelExtremelyHazardous:
				aqiID = "aqi_l7"
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
				addEvent("diff10_l1u")
			}
			if z10 == zoneYellow {
				addEvent("diff10_l2u")
			}
			if z10 == zoneRed {
				addEvent("diff10_l3u")
			}
		}
		// PM2.5 Growth
		if pm25DiffExceeded && p25d > 0 {
			if z25 == zoneGreen {
				addEvent("diff25_l1u")
			}
			if z25 == zoneYellow {
				addEvent("diff25_l2u")
			}
			if z25 == zoneRed {
				addEvent("diff25_l3u")
			}
		}
		// Combined Growth
		if pm10DiffExceeded && p10d > 0 && pm25DiffExceeded && p25d > 0 {
			if z10 == zoneGreen && z25 == zoneGreen {
				addEvent("diffs_l1u")
			}
			if z10 == zoneYellow && z25 == zoneYellow {
				addEvent("diffs_l2u")
			}
			if z10 == zoneRed && z25 == zoneRed {
				addEvent("diffs_l3u")
			}
		}
		// PM10 Drop
		if pm10DiffExceeded && p10d < 0 {
			if z10 == zoneGreen {
				addEvent("diff10_l1d")
			}
			if z10 == zoneYellow {
				addEvent("diff10_l2d")
			}
			if z10 == zoneRed {
				addEvent("diff10_l3d")
			}
		}
		// PM2.5 Drop
		if pm25DiffExceeded && p25d < 0 {
			if z25 == zoneGreen {
				addEvent("diff25_l1d")
			}
			if z25 == zoneYellow {
				addEvent("diff25_l2d")
			}
			if z25 == zoneRed {
				addEvent("diff25_l3d")
			}
		}
		// Combined Drop
		if pm10DiffExceeded && p10d < 0 && pm25DiffExceeded && p25d < 0 {
			if z10 == zoneGreen && z25 == zoneGreen {
				addEvent("diffs_l1d")
			}
			if z10 == zoneYellow && z25 == zoneYellow {
				addEvent("diffs_l2d")
			}
			if z10 == zoneRed && z25 == zoneRed {
				addEvent("diffs_l3d")
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
	// 1. Try RAM first (memcache)
	res := s.GetHistory(deviceID)
	if len(res) >= 2 {
		since := time.Now().Add(-duration)
		// Return RAM only if the earliest record is older or equal to the requested start time
		if res[0].Timestamp.Before(since) || res[0].Timestamp.Equal(since) {
			log.Debug().Str("device", deviceID).Int("count", len(res)).Msg("monitor: history fetched from RAM (covers duration)")
			return res
		}
	}

	// 2. Try SQL if RAM is empty or insufficient
	if s.db != nil {
		res = s.getHistoryFromSQL(deviceID, duration)
		if len(res) > 0 {
			log.Debug().Str("device", deviceID).Int("count", len(res)).Msg("monitor: history fetched from SQL")
			return res
		}
	}
	// 3. Try JSON
	if s.cfg.Database.JsonFile != "" {
		res = s.getHistoryFromJSON(deviceID, duration)
		if len(res) > 0 {
			log.Debug().Str("device", deviceID).Int("count", len(res)).Msg("monitor: history fetched from JSON")
			return res
		}
	}
	return nil
}

func (s *MonitorService) getHistoryFromSQL(deviceID string, duration time.Duration) []Measurement {
	since := time.Now().UTC().Add(-duration)
	log.Debug().Str("device", deviceID).Dur("duration", duration).Msg("db: querying sql history")
	query := "SELECT device_id, timestamp, pm10, pm25, pm10_raw, pm25_raw, temperature, humidity, pressure FROM measurements WHERE device_id = $1 AND timestamp >= $2 ORDER BY timestamp ASC"
	rows, err := s.db.Query(query, deviceID, since)
	if err != nil {
		log.Error().Err(err).Msg("failed to query SQL history")
		return nil
	}
	defer rows.Close()

	var res []Measurement
	for rows.Next() {
		var m Measurement
		if err := rows.Scan(&m.DeviceID, &m.Timestamp, &m.PM10, &m.PM25, &m.PM10Raw, &m.PM25Raw, &m.Temperature, &m.Humidity, &m.Pressure); err == nil {
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
