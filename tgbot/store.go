package tgbot

import (
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
}

// Store manages Telegram bot state persisted to a JSON file.
type Store struct {
	mu   sync.RWMutex
	file string
	subs map[int64]*Subscription // keyed by chat_id
}

// NewStore creates a Store backed by the given file path.
func NewStore(file string) *Store {
	s := &Store{
		file: file,
		subs: make(map[int64]*Subscription),
	}
	s.load()
	return s
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
	list := make([]*Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		list = append(list, sub)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("tgbot: failed to marshal store")
		return
	}
	if err := os.WriteFile(s.file, data, 0644); err != nil {
		log.Error().Err(err).Str("file", s.file).Msg("tgbot: failed to write store file")
	}
}

// GetSettings returns the personalized settings for a chat.
// If the chat is not found, it creates a new subscription with default settings.
func (s *Store) GetSettings(chatID int64, defaults *config.Monitor) *config.Monitor {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[chatID]
	if !ok {
		sub = &Subscription{
			ChatID:   chatID,
			Settings: s.cloneMonitor(defaults),
		}
		s.subs[chatID] = sub
		s.save()
		return sub.Settings
	}
	if sub.Settings == nil {
		sub.Settings = s.cloneMonitor(defaults)
		s.save()
	}
	return sub.Settings
}

func (s *Store) cloneMonitor(m *config.Monitor) *config.Monitor {
	if m == nil {
		return nil
	}
	clone := *m
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
	}
	if sub.Language != lang {
		sub.Language = lang
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

// GetTGCode returns the last synced Telegram language code.
func (s *Store) GetTGCode(chatID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sub, ok := s.subs[chatID]; ok {
		return sub.TGCode
	}
	return ""
}

// Subscribe adds deviceID to chatID's subscription list if not already present.
// Returns true if newly added.
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
// Returns true if it was found and removed.
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

// AllSubscriptions returns a copy of all subscriptions.
func (s *Store) AllSubscriptions() map[int64]Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make(map[int64]Subscription, len(s.subs))
	for k, v := range s.subs {
		res[k] = *v
	}
	return res
}
