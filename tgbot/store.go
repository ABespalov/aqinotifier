package tgbot

import (
	"database/sql"
	"encoding/json"
	"os"
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
	mu               sync.RWMutex
	file             string
	db               *sql.DB
	subs             map[int64]*Subscription // keyed by chat_id
	fileMu           sync.RWMutex            // protects JSON file from concurrent writes
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
	s.db = db
	s.mu.Unlock()

	query := "CREATE TABLE IF NOT EXISTS bot_subscriptions (chat_id BIGINT PRIMARY KEY, data JSONB);"
	_, err := db.Exec(query)
	if err != nil {
		log.Error().Err(err).Msg("tgbot: failed to initialize SQL table")
		return
	}

	log.Info().Msg("tgbot: sql table bot_subscriptions ready")
	s.loadFromSQL()
}

func (s *Store) loadFromSQL() {
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	if db == nil {
		return
	}

	log.Debug().Msg("tgbot: loading subscriptions from sql")
	rows, err := db.Query("SELECT chat_id, data FROM bot_subscriptions")
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
				if sub.Settings != nil {
					sub.Settings.Validate()
					log.Info().Int64("chat_id", chatID).
						Float64("pm25_l1", sub.Settings.PM25L1).
						Float64("pm10_l1", sub.Settings.PM10L1).
						Msg("tgbot: loaded settings from sql")
				} else {
					log.Warn().Int64("chat_id", chatID).Msg("tgbot: loaded subscription from sql but settings are NULL")
				}
				s.mu.Lock()
				s.subs[chatID] = &sub
				s.mu.Unlock()
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
		if sub.Settings != nil {
			sub.Settings.Validate()
		}
		s.subs[sub.ChatID] = sub
	}
}

func (s *Store) saveLocked() {
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
	s.saveToSQLLocked()

	if err := os.WriteFile(s.file, data, 0644); err != nil {
		log.Error().Err(err).Str("file", s.file).Msg("tgbot: failed to write store file")
	} else {
		log.Debug().Str("file", s.file).Int("users", len(s.subs)).Msg("tgbot: store saved to file")
	}
}

func (s *Store) saveToSQLLocked() {
	if s.db == nil {
		return
	}
	// We are already locked, but we want to do IO without lock.
	// So we copy and then run IO.
	copySubs := make(map[int64]*Subscription, len(s.subs))
	for k, v := range s.subs {
		copySubs[k] = v
	}
	db := s.db

	// We can't easily drop s.mu here because of defer in caller.
	// But saveToSQLLocked is only called from saveLocked which is called from
	// methods that don't do much else.
	// For settings updates, we want sync to ensure persistence
	s.performSQLSync(db, copySubs)
}

func (s *Store) performSQLSync(db *sql.DB, subs map[int64]*Subscription) {
	for chatID, sub := range subs {
		data, err := json.Marshal(sub)
		if err != nil {
			continue
		}
		log.Debug().Int64("chat_id", chatID).Msg("tgbot: saving subscription to sql")
		_, err = db.Exec("INSERT INTO bot_subscriptions (chat_id, data) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET data = $2", chatID, data)
		if err != nil {
			log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to save to SQL")
		} else {
			log.Info().Int64("chat_id", chatID).Msg("tgbot: subscription successfully synced to sql")
		}
	}
}

// GetSettings returns the personalized settings for a chat.
func (s *Store) GetSettings(chatID int64, defaults *config.Monitor) *config.Monitor {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok || sub.Settings == nil {
		if !ok {
			sub = &Subscription{
				ChatID:   chatID,
				Settings: s.cloneMonitor(defaults),
				Version:  1,
			}
			s.subs[chatID] = sub
		} else {
			sub.Settings = s.cloneMonitor(defaults)
		}
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered or settings missing")
		s.saveLocked()
		return sub.Settings
	}

	sub.Settings.Validate()
	log.Debug().Int64("chat_id", chatID).
		Float64("pm25_l1", sub.Settings.PM25L1).
		Float64("pm10_l1", sub.Settings.PM10L1).
		Msg("tgbot: returning existing settings")
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
		s.saveLocked()
	}
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
	log.Info().Int64("chat_id", chatID).
		Float64("pm25_l1", settings.PM25L1).
		Float64("pm10_l1", settings.PM10L1).
		Msg("tgbot: UpdateSettings - memory updated, triggering save")
	s.saveLocked()
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
	log.Debug().Int64("chat_id", chatID).Str("lang", lang).Msg("tgbot: SetLanguage start")
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
		s.saveLocked()
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
		s.saveLocked()
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
		s.saveLocked()
	}
}

// SyncLanguage updates the language only if the Telegram language code has changed.
func (s *Store) SyncLanguage(chatID int64, tgCode string, detected string) (changed bool, isNew bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{ChatID: chatID, Language: detected, TGCode: tgCode}
		s.subs[chatID] = sub
		log.Info().Int64("chat_id", chatID).Msg("tgbot: new user registered")
		s.saveLocked()
		return false, true
	}
	if sub.TGCode != tgCode {
		oldLang := sub.Language
		sub.TGCode = tgCode
		sub.Language = detected
		s.saveLocked()
		return oldLang != "" && oldLang != detected, false
	}
	return false, false
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
	s.saveLocked()
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
	s.saveLocked()
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
