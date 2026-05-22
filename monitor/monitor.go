// Package monitor handles device measurement ingestion, evaluation, alert transitions,
// history management (both in-memory caching and persistent storage via JSON / SQL database),
// and notification triggers.
package monitor

import (
	"math"
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
	ID        string
	Value     float64
	PrevValue float64
	HasPrev   bool
}

type MeasurementStore interface {
	SaveMeasurement(m Measurement)
	LoadMeasurements(limit int) map[string][]Measurement
	GetMeasurementsByDuration(deviceID string, duration time.Duration) []Measurement
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

type aqiLazyKey struct {
	ChatID   int64
	DeviceID string
}

type aqiLazyState struct {
	ConfirmedLevel sensor.AQILevel
	UpCounter      int
	DownCounter    int
}

type MonitorService struct {
	cfg        *config.Config
	history    map[string][]Measurement
	mu         sync.RWMutex
	notifier   Notifier
	store      MeasurementStore
	evaluators map[string]*DeviceEvaluator
	lazyStates map[aqiLazyKey]*aqiLazyState
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

func NewMonitorService(cfg *config.Config, store MeasurementStore) *MonitorService {
	evaluatorsMap := make(map[string]*DeviceEvaluator)
	for devPrefix, corr := range cfg.Monitor.Corrections {
		eval, err := buildEvaluator(corr)
		if err != nil {
			log.Fatal().Err(err).Str("device_prefix", devPrefix).Msg("Failed to build formula evaluator")
		}
		evaluatorsMap[devPrefix] = eval
	}

	hist := make(map[string][]Measurement)
	if store != nil {
		max := cfg.System.ValuesInRam
		if max <= 0 {
			max = 10
		}
		hist = store.LoadMeasurements(max)
	}

	s := &MonitorService{
		cfg:        cfg,
		history:    hist,
		evaluators: evaluatorsMap,
		store:      store,
		lazyStates: make(map[aqiLazyKey]*aqiLazyState),
	}

	for id := range s.history {
		s.recalculateDiffsLocked(id)
	}

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
	if s.store != nil {
		s.store.SaveMeasurement(m)
	}
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
		pm10Level1 := mcfg.PM10.Level1
		pm10Level2 := mcfg.PM10.Level2
		if pm10Level2 < pm10Level1 {
			pm10Level2 = pm10Level1
		}
		pm25Level1 := mcfg.PM25.Level1
		pm25Level2 := mcfg.PM25.Level2
		if pm25Level2 < pm25Level1 {
			pm25Level2 = pm25Level1
		}

		z10 := getZone(m.PM10, pm10Level1, pm10Level2)
		z25 := getZone(m.PM25, pm25Level1, pm25Level2)
		prevZ10 := getZone(m.PM10Prev, pm10Level1, pm10Level2)
		prevZ25 := getZone(m.PM25Prev, pm25Level1, pm25Level2)

		notifications := make(map[string]bool)
		for _, n := range config.FlattenNotifications(mcfg.Notifications) {
			notifications[n] = true
		}

		warnings := make(map[string]bool)
		for _, w := range config.FlattenNotifications(mcfg.Warnings) {
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
			if len(val) > 1 {
				evt.PrevValue = val[1]
				evt.HasPrev = true
			}
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
			if len(val) > 1 {
				evt.PrevValue = val[1]
				evt.HasPrev = true
			}
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

		aqi, level = sensor.CalculateAQI(m.PM25, m.PM10, mcfg.AQI.Standard)

		delayUp := 0
		if mcfg.AQI.LazyNotify.Up != nil {
			delayUp = *mcfg.AQI.LazyNotify.Up
		}
		delayDown := 0
		if mcfg.AQI.LazyNotify.Down != nil {
			delayDown = *mcfg.AQI.LazyNotify.Down
		}

		s.mu.Lock()
		stateKey := aqiLazyKey{ChatID: chatID, DeviceID: m.DeviceID}
		state, exists := s.lazyStates[stateKey]
		if !exists {
			state = &aqiLazyState{}
			s.lazyStates[stateKey] = state
		}

		shouldNotify := false
		var targetLevel sensor.AQILevel
		var oldConfirmedLevel sensor.AQILevel

		if state.ConfirmedLevel == 0 {
			state.ConfirmedLevel = level
			state.UpCounter = 0
			state.DownCounter = 0
		} else if delayUp == 0 && delayDown == 0 {
			if level != state.ConfirmedLevel {
				oldConfirmedLevel = state.ConfirmedLevel
				state.ConfirmedLevel = level
				shouldNotify = true
				targetLevel = level
			}
		} else {
			// Update counters based on trend
			if level > state.ConfirmedLevel {
				state.UpCounter++
				state.DownCounter = 0
			} else if level < state.ConfirmedLevel {
				state.DownCounter++
				state.UpCounter = 0
			} else {
				state.UpCounter = 0
				state.DownCounter = 0
			}

			// Check UP notification
			effectiveDelayUp := delayUp
			if effectiveDelayUp <= 0 {
				effectiveDelayUp = 1
			}
			if state.UpCounter >= effectiveDelayUp && level > state.ConfirmedLevel {
				oldConfirmedLevel = state.ConfirmedLevel
				state.ConfirmedLevel = level
				state.UpCounter = 0
				state.DownCounter = 0
				shouldNotify = true
				targetLevel = level
			}

			// Check DOWN notification
			effectiveDelayDown := delayDown
			if effectiveDelayDown <= 0 {
				effectiveDelayDown = 1
			}
			if state.DownCounter >= effectiveDelayDown && level < state.ConfirmedLevel {
				oldConfirmedLevel = state.ConfirmedLevel
				state.ConfirmedLevel = level
				state.DownCounter = 0
				state.UpCounter = 0
				shouldNotify = true
				targetLevel = level
			}
		}
		s.mu.Unlock()

		if shouldNotify {
			var aqiID string
			switch targetLevel {
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
				if targetLevel == sensor.LevelGood {
					addClear(aqiID, aqi, float64(oldConfirmedLevel))
				} else {
					addEvent(aqiID, aqi, float64(oldConfirmedLevel))
				}
			}
		}

		// Silent Notifications (Growth/Drop within zones)
		pm10DiffExceeded := m.PM10Diff != nil && math.Abs(*m.PM10Diff) >= mcfg.PM10.Diff
		pm25DiffExceeded := m.PM25Diff != nil && math.Abs(*m.PM25Diff) >= mcfg.PM25.Diff

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

	// 2. Try SQL/JSON via store
	if s.store != nil {
		res = s.store.GetMeasurementsByDuration(deviceID, duration)
		if len(res) > 0 {
			log.Debug().Str("device", deviceID).Int("count", len(res)).Msg("monitor: history fetched from persistent store")
			return res
		}
	}
	return nil
}

func (s *MonitorService) Close() {}
