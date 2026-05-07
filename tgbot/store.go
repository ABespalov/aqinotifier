package tgbot

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/rs/zerolog/log"
)

// Subscription holds per-chat subscriptions and personalized settings.
type Subscription struct {
	ChatID    int64           `json:"chat_id"`
	DeviceIDs []string        `json:"device_ids"`
	Settings  *config.Monitor `json:"settings,omitempty"`
	Language  string          `json:"language,omitempty"`
	TGCode    string          `json:"tg_code,omitempty"`
	UnitTemp  string          `json:"unit_temp,omitempty"`  // "c", "f"
	UnitPress string          `json:"unit_press,omitempty"` // "mmhg", "hpa"
	Version   int             `json:"version,omitempty"`    // for migrations
}

// Store manages Telegram bot state persisted to a JSON file or Postgres.
type Store struct {
	mu     sync.RWMutex
	file   string
	db     *sql.DB
	subs   map[int64]*Subscription // keyed by chat_id
	fileMu sync.RWMutex           // protects JSON file from concurrent writes
	defaultUnitTemp  string
	defaultUnitPress string
}

// NewStore creates a Store backed by the given file path.
func NewStore(file string, defaultUnitTemp, defaultUnitPress string) *Store {
	s := &Store{
		file:             file,
		subs:             make(map[int64]*Subscription),
		defaultUnitTemp:  defaultUnitTemp,
		defaultUnitPress: defaultUnitPress,
	}
	s.load()
	return s
}

// SetDB attaches a Postgres connection and migrates/syncs data.
func (s *Store) SetDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db

	query := "CREATE TABLE IF NOT EXISTS bot_subscriptions (chat_id BIGINT PRIMARY KEY, data JSONB);"
	_, err := s.db.Exec(query)
	if err != nil {
		log.Error().Err(err).Msg("tgbot: failed to initialize SQL table")
	} else {
		log.Info().Msg("tgbot: sql table bot_subscriptions ready")
		s.loadFromSQL()
		// Migration/Sync: ensure SQL has everything that RAM (from JSON) had
		if len(s.subs) > 0 {
			log.Info().Int("count", len(s.subs)).Msg("tgbot: syncing data to postgres")
			s.saveToSQL()
		}
	}
}

// SyncDB attempts to reconcile RAM/JSON data with Postgres.
func (s *Store) SyncDB() {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return
	}
	if err := db.Ping(); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveToSQL()
}

func (s *Store) loadFromSQL() {
	log.Debug().Msg("tgbot: loading subscriptions from sql")
	rows, err := s.db.Query("SELECT chat_id, data FROM bot_subscriptions")
	if err != nil {
		log.Error().Err(err).Msg("tgbot: failed to load from SQL")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var chatID int64
		var data []byte
		if err := rows.Scan(&chatID, &data); err == nil {
			var sub Subscription
			if err := json.Unmarshal(data, &sub); err == nil {
				s.subs[chatID] = &sub
			}
		}
	}
}

func (s *Store) load() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Err(err).Str("file", s.file).Msg("tgbot: failed to read store file")
		}
		return
	}
	var list []*Subscription
	if err := json.Unmarshal(data, &list); err != nil {
		log.Error().Err(err).Str("file", s.file).Msg("tgbot: failed to unmarshal store")
		return
	}
	for _, sub := range list {
		s.subs[sub.ChatID] = sub
	}
}

func (s *Store) save() {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	list := make([]*Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		list = append(list, sub)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("tgbot: failed to marshal store")
		return
	}
	
	// Always sync to SQL if available
	s.saveToSQL()

	if err := os.WriteFile(s.file, data, 0644); err != nil {
		log.Error().Err(err).Str("file", s.file).Msg("tgbot: failed to write store file")
	} else {
		log.Debug().Str("file", s.file).Int("users", len(s.subs)).Msg("tgbot: store saved to file")
	}
}

func (s *Store) saveToSQL() {
	if s.db == nil {
		return
	}
	for chatID, sub := range s.subs {
		data, err := json.Marshal(sub)
		if err != nil {
			continue
		}
		log.Debug().Int64("chat_id", chatID).Msg("tgbot: saving subscription to sql")
		_, err = s.db.Exec("INSERT INTO bot_subscriptions (chat_id, data) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET data = $2", chatID, data)
		if err != nil {
			log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to save to SQL")
		}
	}
}

// GetSettings returns the personalized settings for a chat.
func (s *Store) GetSettings(chatID int64, defaults *config.Monitor) *config.Monitor {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{
			ChatID:   chatID,
			Settings: s.cloneMonitor(defaults),
			Version:  1,
		}
		s.subs[chatID] = sub
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered")
		s.save()
		return sub.Settings
	}
	// Forced reset for old users during "update"
	if sub.Version == 0 {
		log.Info().Int64("chat_id", chatID).Msg("tgbot: resetting old user settings to defaults")
		sub.Settings = s.cloneMonitor(defaults)
		sub.Version = 1
		s.save()
		return sub.Settings
	}
	if sub.Settings == nil {
		sub.Settings = s.cloneMonitor(defaults)
		s.save()
	} else {
		if s.migrateSettings(sub.Settings, defaults) {
			s.save()
		}
	}
	return sub.Settings
}

// ResetSettings clears user settings and restores defaults.
func (s *Store) ResetSettings(chatID int64, defaults *config.Monitor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if ok {
		sub.Settings = s.cloneMonitor(defaults)
		sub.UnitTemp = s.defaultUnitTemp
		sub.UnitPress = s.defaultUnitPress
		sub.Version = 1
		s.save()
	}
}

func (s *Store) migrateSettings(m *config.Monitor, defaults *config.Monitor) bool {
	changed := false

	// 1. Thresholds migration
	if m.PM10Green == 0 && m.PM10Value > 0 {
		m.PM10Green = m.PM10Value
		m.PM10Value = 0
		changed = true
	}
	if m.PM25Green == 0 && m.PM25Value > 0 {
		m.PM25Green = m.PM25Value
		m.PM25Value = 0
		changed = true
	}
	if m.PM10Yellow == 0 {
		m.PM10Yellow = defaults.PM10Yellow
		changed = true
	}
	if m.PM25Yellow == 0 {
		m.PM25Yellow = defaults.PM25Yellow
		changed = true
	}

	// 2. Notifications/Warnings migration (Old Schema to New Schema)
	// We detect old schema if Notifications is empty but we have data in Warnings (which was 'active list')
	// or OldLoudWarnings is populated.
	if len(m.Notifications) == 0 && (len(m.Warnings) > 0 || len(m.OldLoudWarnings) > 0) {
		log.Info().Msg("tgbot: migrating user to new notification schema")
		
		mapAQI := func(id string) string {
			switch id {
			case "aqi_g": return "aqi_z1"
			case "aqi_y": return "aqi_z2"
			case "aqi_o": return "aqi_z3"
			case "aqi_r": return "aqi_z4"
			case "aqi_v": return "aqi_z5"
			case "aqi_b": return "aqi_z6"
			default: return id
			}
		}

		// New Notifications list: all transitions (val10, val25, vals) 
		// and AQI that were loud in old schema.
		newNotes := []string{
			"val10-yu", "val10-ru", "val10-yd", "val10-gd",
			"val25-yu", "val25-ru", "val25-yd", "val25-gd",
			"vals-yu", "vals-ru", "vals-yd", "vals-gd",
		}
		
		// Add previously loud AQI alerts (with mapping)
		for _, lw := range m.OldLoudWarnings {
			if strings.HasPrefix(lw, "aqi_") {
				newNotes = append(newNotes, mapAQI(lw))
			}
		}
		m.Notifications = newNotes

		// New Warnings list: default to "everything loud" as per user request
		m.Warnings = []string{
			"val10-yu", "val10-ru", "val10-yd", "val10-gd",
			"val25-yu", "val25-ru", "val25-yd", "val25-gd",
			"vals-yu", "vals-ru", "vals-yd", "vals-gd",
			"aqi_z1", "aqi_z2", "aqi_z3", "aqi_z4", "aqi_z5", "aqi_z6", "aqi_z7",
		}
		
		m.OldLoudWarnings = nil // Clear migration field
		changed = true
	}

	// 2b. Map remaining old AQI IDs if any (for intermediate versions)
	for i, n := range m.Notifications {
		if strings.HasPrefix(n, "aqi_") && !strings.Contains(n, "_z") {
			switch n {
			case "aqi_g": m.Notifications[i] = "aqi_z1"
			case "aqi_y": m.Notifications[i] = "aqi_z2"
			case "aqi_o": m.Notifications[i] = "aqi_z3"
			case "aqi_r": m.Notifications[i] = "aqi_z4"
			case "aqi_v": m.Notifications[i] = "aqi_z5"
			case "aqi_b": m.Notifications[i] = "aqi_z6"
			}
			changed = true
		}
	}
	for i, w := range m.Warnings {
		if strings.HasPrefix(w, "aqi_") && !strings.Contains(w, "_z") {
			switch w {
			case "aqi_g": m.Warnings[i] = "aqi_z1"
			case "aqi_y": m.Warnings[i] = "aqi_z2"
			case "aqi_o": m.Warnings[i] = "aqi_z3"
			case "aqi_r": m.Warnings[i] = "aqi_z4"
			case "aqi_v": m.Warnings[i] = "aqi_z5"
			case "aqi_b": m.Warnings[i] = "aqi_z6"
			}
			changed = true
		}
	}

	// 3. AQI settings migration
	if m.AQIStandard == "" {
		m.AQIStandard = defaults.AQIStandard
		changed = true
	}
	if len(m.Notifications) == 0 {
		m.Notifications = make([]string, len(defaults.Notifications))
		copy(m.Notifications, defaults.Notifications)
		changed = true
	}
	if len(m.Warnings) == 0 {
		m.Warnings = make([]string, len(defaults.Warnings))
		copy(m.Warnings, defaults.Warnings)
		changed = true
	}

	return changed
}

func (s *Store) cloneMonitor(m *config.Monitor) *config.Monitor {
	if m == nil {
		return nil
	}
	clone := *m
	clone.Notifications = make([]string, len(m.Notifications))
	copy(clone.Notifications, m.Notifications)
	clone.Warnings = make([]string, len(m.Warnings))
	copy(clone.Warnings, m.Warnings)
	return &clone
}

// UpdateSettings updates the personalized settings for a chat.
func (s *Store) UpdateSettings(chatID int64, settings *config.Monitor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{ChatID: chatID}
		s.subs[chatID] = sub
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered")
	}
	sub.Settings = settings
	s.save()
}



// GetLanguage returns the language code for a chat.
func (s *Store) GetLanguage(chatID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sub, ok := s.subs[chatID]; ok {
		return sub.Language
	}
	return ""
}

// SetLanguage updates the language code for a chat.
func (s *Store) SetLanguage(chatID int64, lang string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{ChatID: chatID}
		s.subs[chatID] = sub
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered")
	}
	if sub.Language != lang {
		sub.Language = lang
		s.save()
	}
}

func (s *Store) GetUnitTemp(chatID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sub, ok := s.subs[chatID]; ok && sub.UnitTemp != "" {
		return sub.UnitTemp
	}
	return s.defaultUnitTemp
}

func (s *Store) SetUnitTemp(chatID int64, unit string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{ChatID: chatID}
		s.subs[chatID] = sub
	}
	if sub.UnitTemp != unit {
		sub.UnitTemp = unit
		s.save()
	}
}

func (s *Store) GetUnitPress(chatID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sub, ok := s.subs[chatID]; ok && sub.UnitPress != "" {
		return sub.UnitPress
	}
	return s.defaultUnitPress
}

func (s *Store) SetUnitPress(chatID int64, unit string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{ChatID: chatID}
		s.subs[chatID] = sub
	}
	if sub.UnitPress != unit {
		sub.UnitPress = unit
		s.save()
	}
}

// SyncLanguage updates the language only if the Telegram language code has changed.
func (s *Store) SyncLanguage(chatID int64, tgCode string, detected string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{ChatID: chatID, Language: detected, TGCode: tgCode}
		s.subs[chatID] = sub
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered")
		s.save()
		return false
	}
	if sub.TGCode != tgCode {
		oldLang := sub.Language
		sub.TGCode = tgCode
		sub.Language = detected
		s.save()
		return oldLang != "" && oldLang != detected
	}
	return false
}



// Subscribe adds deviceID to chatID's subscription list if not already present.
func (s *Store) Subscribe(chatID int64, deviceID string, defaults *config.Monitor) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{
			ChatID:   chatID,
			Settings: s.cloneMonitor(defaults),
		}
		s.subs[chatID] = sub
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered")
	}
	if sub.Settings == nil {
		sub.Settings = s.cloneMonitor(defaults)
	}

	for _, id := range sub.DeviceIDs {
		if id == deviceID {
			return false
		}
	}
	sub.DeviceIDs = append(sub.DeviceIDs, deviceID)
	s.save()
	return true
}

// Unsubscribe removes deviceID from chatID's subscription list.
func (s *Store) Unsubscribe(chatID int64, deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		return false
	}
	newIDs := sub.DeviceIDs[:0]
	found := false
	for _, id := range sub.DeviceIDs {
		if id == deviceID {
			found = true
			continue
		}
		newIDs = append(newIDs, id)
	}
	if !found {
		return false
	}
	sub.DeviceIDs = newIDs
	s.save()
	return true
}

// Subscriptions returns the device IDs subscribed by chatID.
func (s *Store) Subscriptions(chatID int64) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.subs[chatID]
	if !ok {
		return nil
	}
	result := make([]string, len(sub.DeviceIDs))
	copy(result, sub.DeviceIDs)
	return result
}

// Subscribers returns all chat IDs subscribed to deviceID.
func (s *Store) Subscribers(deviceID string) []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var chats []int64
	for chatID, sub := range s.subs {
		for _, id := range sub.DeviceIDs {
			if id == deviceID {
				chats = append(chats, chatID)
				break
			}
		}
	}
	return chats
}


