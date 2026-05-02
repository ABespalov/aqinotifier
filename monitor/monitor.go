package monitor

import (
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
	mu       sync.Mutex
	notifier Notifier
	// alertStates tracks which absolute-threshold alert keys are currently
	// active per user per device.
	// chatID -> deviceID -> alertKey -> bool
	alertStates map[int64]map[string]map[string]bool
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
	s.loadHistory()
	return s
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
	for _, m := range all {
		s.history[m.DeviceID] = append(s.history[m.DeviceID], m)
	}

	// Reconstruct initial alert states is skipped here because we don't have
	// access to user settings yet (notifier is nil during NewMonitorService).
	// They will be initialized on the first data point for each user.
}

func (s *MonitorService) saveHistory() {
	if s.cfg.Database.Type != "json" || s.cfg.Database.JsonFile == "" {
		return
	}
	var all []Measurement
	for _, ms := range s.history {
		all = append(all, ms...)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal history for saving")
		return
	}
	err = os.WriteFile(s.cfg.Database.JsonFile, data, 0644)
	if err != nil {
		log.Error().Err(err).Str("file", s.cfg.Database.JsonFile).Msg("failed to write history file")
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
	s.saveHistory()

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

		var messages []string
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
				messages = append(messages, s.notifier.T(chatID, "alert_val10_exceeded",
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
				messages = append(messages, s.notifier.T(chatID, "alert_val25_exceeded",
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
				messages = append(messages, s.notifier.T(chatID, "alert_vals_exceeded",
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
			messages = append(messages, s.notifier.T(chatID, "alert_diff10_growth",
				*m.PM10Diff, mcfg.PM10Diff, m.DiffTime, m.PM10Prev, m.PM10))
		}
		// diff25: PM2.5 percentage growth >= pm25_diff
		if warnings["diff25"] && pm25DiffExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diff25_growth",
				*m.PM25Diff, mcfg.PM25Diff, m.DiffTime, m.PM25Prev, m.PM25))
		}
		// diffs: both PM10 and PM2.5 increase simultaneously by their respective diff values
		if warnings["diffs"] && pm10DiffExceeded && pm25DiffExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diffs_growth",
				*m.PM10Diff, *m.PM25Diff, m.DiffTime))
		}

		// diff10_neg, diff25_neg, diffs_neg
		pm10NegDiffExceeded := m.PM10Diff != nil && *m.PM10Diff <= -mcfg.PM10Diff
		pm25NegDiffExceeded := m.PM25Diff != nil && *m.PM25Diff <= -mcfg.PM25Diff

		// diff10_neg: PM10 percentage decrease >= pm10_diff
		if warnings["diff10_neg"] && pm10NegDiffExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diff10_decrease",
				*m.PM10Diff, m.DiffTime, m.PM10Prev, m.PM10))
		}
		// diff25_neg: PM2.5 percentage decrease >= pm25_diff
		if warnings["diff25_neg"] && pm25NegDiffExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diff25_decrease",
				*m.PM25Diff, m.DiffTime, m.PM25Prev, m.PM25))
		}
		// diffs_neg: both PM10 and PM2.5 decrease simultaneously by their respective diff values
		if warnings["diffs_neg"] && pm10NegDiffExceeded && pm25NegDiffExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diffs_decrease",
				*m.PM10Diff, *m.PM25Diff, m.DiffTime))
		}

		// diff10_over, diff25_over, diffs_over
		// diff10_over: PM10 growth >= pm10_diff while already above absolute threshold
		if warnings["diff10_over"] && pm10DiffExceeded && pm10AbsExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diff10_crit",
				*m.PM10Diff, m.DiffTime, m.PM10))
		}
		// diff25_over: PM2.5 growth >= pm25_diff while already above absolute threshold
		if warnings["diff25_over"] && pm25DiffExceeded && pm25AbsExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diff25_crit",
				*m.PM25Diff, m.DiffTime, m.PM25))
		}
		// diffs_over: both PM10/PM2.5 growth >= diff values while both are above absolute thresholds
		if warnings["diffs_over"] && pm10DiffExceeded && pm25DiffExceeded && pm10AbsExceeded && pm25AbsExceeded {
			messages = append(messages, s.notifier.T(chatID, "alert_diffs_crit",
				*m.PM10Diff, *m.PM25Diff))
		}

		// diff10_neg_over, diff25_neg_over, diffs_neg_over
		// diff10_neg_over: PM10 decrease >= pm10_diff resulting in value below absolute threshold
		if warnings["diff10_neg_over"] && pm10NegDiffExceeded && !pm10AbsExceeded {
			isTransition = true
			messages = append(messages, s.notifier.T(chatID, "alert_diff10_clean",
				*m.PM10Diff, m.PM10))
		}
		// diff25_neg_over: PM2.5 decrease >= pm25_diff resulting in value below absolute threshold
		if warnings["diff25_neg_over"] && pm25NegDiffExceeded && !pm25AbsExceeded {
			isTransition = true
			messages = append(messages, s.notifier.T(chatID, "alert_diff25_clean",
				*m.PM25Diff, m.PM25))
		}
		// diffs_neg_over: both PM10/PM2.5 decrease >= diff values resulting in both values below thresholds
		if warnings["diffs_neg_over"] && pm10NegDiffExceeded && pm25NegDiffExceeded && !pm10AbsExceeded && !pm25AbsExceeded {
			isTransition = true
			messages = append(messages, s.notifier.T(chatID, "alert_diffs_clean",
				*m.PM10Diff, *m.PM25Diff))
		}

		if len(messages) > 0 || len(clearMessages) > 0 {
			for _, msg := range messages {
				log.Warn().Int64("chat", chatID).Str("device", m.DeviceID).Msg(msg)
			}
			for _, msg := range clearMessages {
				log.Info().Int64("chat", chatID).Str("device", m.DeviceID).Msg("cleared: " + msg)
			}
			// Use the unified Notify method.
			// It should be loud if there's a transition or a clear message.
			s.notifier.Notify(chatID, m.DeviceID, m, messages, clearMessages, !isTransition && len(clearMessages) == 0)
		}
	}
}
