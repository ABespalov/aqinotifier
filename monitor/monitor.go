package monitor

import (
	"encoding/json"
	"fmt"
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
	// SendWarning is called when a threshold is newly exceeded.
	// The silent flag indicates if the message should be delivered without sound.
	SendWarning(deviceID string, m *Measurement, messages []string, silent bool)
	// SendClear is called when a previously active absolute-threshold alert
	// has cleared (values returned to normal).
	SendClear(deviceID string, m *Measurement, messages []string)
}

type MonitorService struct {
	cfg         *config.Config
	history     map[string][]Measurement
	mu          sync.Mutex
	notifier    Notifier
	// alertStates tracks which absolute-threshold alert keys are currently
	// active per device, enabling edge-triggered (one-shot) notifications.
	alertStates map[string]map[string]bool
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
		alertStates: make(map[string]map[string]bool),
	}
	s.loadHistory()
	return s
}

// isAlertActive returns true if the given alert key is currently active for
// the device. Must be called with s.mu held.
func (s *MonitorService) isAlertActive(deviceID, key string) bool {
	if m, ok := s.alertStates[deviceID]; ok {
		return m[key]
	}
	return false
}

// setAlertActive updates the alert state for key/device.
// Must be called with s.mu held.
func (s *MonitorService) setAlertActive(deviceID, key string, active bool) {
	if _, ok := s.alertStates[deviceID]; !ok {
		s.alertStates[deviceID] = make(map[string]bool)
	}
	s.alertStates[deviceID][key] = active
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

	// Reconstruct initial alert states from the last measurement of each device.
	// This prevents re-triggering absolute alerts upon server restart if the
	// pollution level is still high.
	for deviceID, hist := range s.history {
		if len(hist) == 0 {
			continue
		}
		m := hist[len(hist)-1]
		pm10AbsExceeded := m.PM10 > s.cfg.Monitor.PM10Value
		pm25AbsExceeded := m.PM25 > s.cfg.Monitor.PM25Value

		// Reconstruction mirrors the notify logic (vals priority, etc.)
		warnings := make(map[string]bool)
		for _, w := range s.cfg.Monitor.Warnings {
			warnings[w] = true
		}

		if warnings["vals"] && pm10AbsExceeded && pm25AbsExceeded {
			s.setAlertActive(deviceID, "vals", true)
		}
		if warnings["val10"] && pm10AbsExceeded {
			s.setAlertActive(deviceID, "val10", true)
		}
		if warnings["val25"] && pm25AbsExceeded {
			s.setAlertActive(deviceID, "val25", true)
		}
	}
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
		// Берутся показания макимально близкие по разнице времени к этому значению
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
	var messages []string
	var clearMessages []string
	isTransition := false

	warnings := make(map[string]bool)
	for _, w := range s.cfg.Monitor.Warnings {
		warnings[w] = true
	}

	pm10AbsExceeded := m.PM10 >= s.cfg.Monitor.PM10Value
	pm25AbsExceeded := m.PM25 >= s.cfg.Monitor.PM25Value

	// val10, val25, vals — edge-triggered: fire only on state change.
	// When vals is enabled it can be included alongside val10/val25.
	
	// val10: при достижении абсолютных значений PM10. При достижении и превышении порога - красная зона, при значениях меньше поргового - зеленая зона
	if warnings["val10"] {
		wasActive := s.isAlertActive(m.DeviceID, "val10")
		if pm10AbsExceeded && !wasActive {
			s.setAlertActive(m.DeviceID, "val10", true)
			isTransition = true
			messages = append(messages, fmt.Sprintf(
				"PM10 превысил порог: %.2f >= %.2f",
				m.PM10, s.cfg.Monitor.PM10Value))
		} else if !pm10AbsExceeded && wasActive {
			s.setAlertActive(m.DeviceID, "val10", false)
			clearMessages = append(clearMessages, fmt.Sprintf(
				"PM10 (%.2f) вернулся в норму (< %.2f)", m.PM10, s.cfg.Monitor.PM10Value))
		}
	}
	// val25: при достижении абсолютных значений PM2.5. При достижении и превышении порога - красная зона, при значениях меньше поргового - зеленая зона
	if warnings["val25"] {
		wasActive := s.isAlertActive(m.DeviceID, "val25")
		if pm25AbsExceeded && !wasActive {
			s.setAlertActive(m.DeviceID, "val25", true)
			isTransition = true
			messages = append(messages, fmt.Sprintf(
				"PM2.5 превысил порог: %.2f >= %.2f",
				m.PM25, s.cfg.Monitor.PM25Value))
		} else if !pm25AbsExceeded && wasActive {
			s.setAlertActive(m.DeviceID, "val25", false)
			clearMessages = append(clearMessages, fmt.Sprintf(
				"PM2.5 (%.2f) вернулся в норму (< %.2f)", m.PM25, s.cfg.Monitor.PM25Value))
		}
	}
	// vals: при достижении и превышении обоих порогов (и PM10, и PM2.5). 
	if warnings["vals"] {
		bothExceeded := pm10AbsExceeded && pm25AbsExceeded
		wasActive := s.isAlertActive(m.DeviceID, "vals")
		if bothExceeded && !wasActive {
			s.setAlertActive(m.DeviceID, "vals", true)
			isTransition = true
			messages = append(messages, fmt.Sprintf(
				"PM10 (%.2f) и PM2.5 (%.2f) превысили пороговые значения",
				m.PM10, m.PM25))
		} else if !bothExceeded && wasActive {
			s.setAlertActive(m.DeviceID, "vals", false)
			clearMessages = append(clearMessages, fmt.Sprintf(
				"PM10 (%.2f) и PM2.5 (%.2f) вернулись в норму",
				m.PM10, m.PM25))
		}
	}

	// diff10, diff25, diffs
	pm10DiffExceeded := m.PM10Diff != nil && *m.PM10Diff >= s.cfg.Monitor.PM10Diff
	pm25DiffExceeded := m.PM25Diff != nil && *m.PM25Diff >= s.cfg.Monitor.PM25Diff

	// diff10: при увеличении PM10 на велечину, большую или равную pm10_diff
	if warnings["diff10"] && pm10DiffExceeded {
		messages = append(messages, fmt.Sprintf(
			"Резкий рост PM10: %.1f%% >= %.1f%% за %dс (было: %.2f, стало: %.2f)",
			*m.PM10Diff, s.cfg.Monitor.PM10Diff, m.DiffTime, m.PM10Prev, m.PM10))
	}
	// diff25: при увеличении PM2.5 на велечину, большую или равную pm25_diff
	if warnings["diff25"] && pm25DiffExceeded {
		messages = append(messages, fmt.Sprintf(
			"Резкий рост PM2.5: %.1f%% >= %.1f%% за %dс (было: %.2f, стало: %.2f)",
			*m.PM25Diff, s.cfg.Monitor.PM25Diff, m.DiffTime, m.PM25Prev, m.PM25))
	}
	// diffs: при увеличении обоих значений одновременно на соответствующие величины
	if warnings["diffs"] && pm10DiffExceeded && pm25DiffExceeded {
		messages = append(messages, fmt.Sprintf(
			"Резкий рост PM10 (%.1f%%) и PM2.5 (%.1f%%) за %dс",
			*m.PM10Diff, *m.PM25Diff, m.DiffTime))
	}

	// diff10_neg, diff25_neg, diffs_neg
	pm10NegDiffExceeded := m.PM10Diff != nil && *m.PM10Diff <= -s.cfg.Monitor.PM10Diff
	pm25NegDiffExceeded := m.PM25Diff != nil && *m.PM25Diff <= -s.cfg.Monitor.PM25Diff

	// diff10_neg: при уменьшении PM10 на велечину, большую или равную pm10_diff
	if warnings["diff10_neg"] && pm10NegDiffExceeded {
		messages = append(messages, fmt.Sprintf(
			"Резкое снижение PM10: %.1f%% за %dс (было: %.2f, стало: %.2f)",
			*m.PM10Diff, m.DiffTime, m.PM10Prev, m.PM10))
	}
	// diff25_neg: при уменьшении PM2.5 на велечину, большую или равную pm25_diff
	if warnings["diff25_neg"] && pm25NegDiffExceeded {
		messages = append(messages, fmt.Sprintf(
			"Резкое снижение PM2.5: %.1f%% за %dс (было: %.2f, стало: %.2f)",
			*m.PM25Diff, m.DiffTime, m.PM25Prev, m.PM25))
	}
	// diffs_neg: при уменьшении обоих значений одновременно на соответствующие величины
	if warnings["diffs_neg"] && pm10NegDiffExceeded && pm25NegDiffExceeded {
		messages = append(messages, fmt.Sprintf(
			"Резкое снижение PM10 (%.1f%%) и PM2.5 (%.1f%%) за %dс",
			*m.PM10Diff, *m.PM25Diff, m.DiffTime))
	}

	// diff10_over, diff25_over, diffs_over
	// diff10_over: при увеличении PM10 на велечину, большую или равную pm10_diff и, одновременно, при превышении PM10 порга
	if warnings["diff10_over"] && pm10DiffExceeded && pm10AbsExceeded {
		messages = append(messages, fmt.Sprintf(
			"Критический рост PM10 в зоне загрязнения: %.1f%% за %dс (стало %.2f)",
			*m.PM10Diff, m.DiffTime, m.PM10))
	}
	// diff25_over: при увеличении PM2.5 на велечину, большую или равную pm25_diff и, одновременно, при превышении PM2.5 порга
	if warnings["diff25_over"] && pm25DiffExceeded && pm25AbsExceeded {
		messages = append(messages, fmt.Sprintf(
			"Критический рост PM2.5 в зоне загрязнения: %.1f%% за %dс (стало %.2f)",
			*m.PM25Diff, m.DiffTime, m.PM25))
	}
	// diffs_over: при увеличении обоих значений одновременно на соответствующие величины и, одновременно, при превышении обоих значений порогов
	if warnings["diffs_over"] && pm10DiffExceeded && pm25DiffExceeded && pm10AbsExceeded && pm25AbsExceeded {
		messages = append(messages, fmt.Sprintf(
			"Критический рост показателей в зоне загрязнения (PM10: %.1f%%, PM2.5: %.1f%%)",
			*m.PM10Diff, *m.PM25Diff))
	}

	// diff10_neg_over, diff25_neg_over, diffs_neg_over
	// diff10_neg_over: при уменшении PM10 на велечину, большую или равную pm10_diff и, одновременно, при значении PM10 ниже порга
	if warnings["diff10_neg_over"] && pm10NegDiffExceeded && !pm10AbsExceeded {
		isTransition = true
		messages = append(messages, fmt.Sprintf(
			"Резкое снижение PM10 в чистую зону: %.1f%% (стало %.2f)",
			*m.PM10Diff, m.PM10))
	}
	// diff25_neg_over: при уменьшени PM2.5 на велечину, большую или равную pm25_diff и, одновременно, при значении PM2.5 ниже порга
	if warnings["diff25_neg_over"] && pm25NegDiffExceeded && !pm25AbsExceeded {
		isTransition = true
		messages = append(messages, fmt.Sprintf(
			"Резкое снижение PM2.5 в чистую зону: %.1f%% (стало %.2f)",
			*m.PM25Diff, m.PM25))
	}
	// diffs_neg_over: при уменьшении обоих значений одновременно на соответствующие величины и, одновременно, при обоих значениях ниже порогов
	if warnings["diffs_neg_over"] && pm10NegDiffExceeded && pm25NegDiffExceeded && !pm10AbsExceeded && !pm25AbsExceeded {
		isTransition = true
		messages = append(messages, fmt.Sprintf(
			"Резкое снижение показателей в чистую зону (PM10: %.1f%%, PM2.5: %.1f%%)",
			*m.PM10Diff, *m.PM25Diff))
	}

	if len(messages) > 0 {
		for _, msg := range messages {
			log.Warn().Str("device", m.DeviceID).Msg(msg)
		}
		if s.notifier != nil {
			s.notifier.SendWarning(m.DeviceID, m, messages, !isTransition)
		}
	}

	if len(clearMessages) > 0 {
		for _, msg := range clearMessages {
			log.Info().Str("device", m.DeviceID).Msg("cleared: " + msg)
		}
		if s.notifier != nil {
			s.notifier.SendClear(m.DeviceID, m, clearMessages)
		}
	}
}
