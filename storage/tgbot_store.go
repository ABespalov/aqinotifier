// This file implements persistence for Telegram bot user settings and subscriptions.
package storage

import (
	"encoding/json"
	"os"

	"github.com/ABespalov/aqinotifier/tgbot"
	"github.com/rs/zerolog/log"
)

func (s *Storage) initTgBotTableLocked() {
	if s.db == nil {
		return
	}
	query := "CREATE TABLE IF NOT EXISTS bot_subscriptions (chat_id BIGINT PRIMARY KEY, data JSONB);"
	_, err := s.db.Exec(query)
	if err != nil {
		log.Error().Err(err).Msg("storage: failed to initialize SQL table for bot_subscriptions")
	}
}

func (s *Storage) syncTgBotJSON() {
	s.tgbotFileMu.Lock()
	defer s.tgbotFileMu.Unlock()

	s.mu.RLock()
	jsonFile := s.cfg.TgBot.File.Json
	db := s.db
	s.mu.RUnlock()

	if jsonFile == "" || db == nil {
		return
	}

	data, err := os.ReadFile(jsonFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Err(err).Str("file", jsonFile).Msg("storage: failed to read JSON subscriptions for sync")
		}
		return
	}

	var list []*tgbot.Subscription
	if err := json.Unmarshal(data, &list); err != nil {
		log.Error().Err(err).Str("file", jsonFile).Msg("storage: failed to unmarshal JSON subscriptions for sync")
		return
	}

	if len(list) == 0 {
		return
	}

	log.Info().Int("count", len(list)).Msg("storage: syncing JSON subscriptions to SQL...")

	tx, err := db.Begin()
	if err != nil {
		log.Error().Err(err).Msg("storage: failed to begin sync transaction for subscriptions")
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO bot_subscriptions (chat_id, data) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET data = $2")
	if err != nil {
		log.Error().Err(err).Msg("storage: failed to prepare sync statement for subscriptions")
		return
	}
	defer stmt.Close()

	for _, sub := range list {
		subData, err := json.Marshal(sub)
		if err != nil {
			continue
		}
		_, err = stmt.Exec(sub.ChatID, subData)
		if err != nil {
			log.Error().Err(err).Int64("chat_id", sub.ChatID).Msg("storage: failed to execute sync statement for sub")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Msg("storage: failed to commit sync transaction for subscriptions")
		return
	}

	log.Info().Int("count", len(list)).Msg("storage: bot subscriptions sync completed, clearing JSON file")

	emptyJSON, _ := json.Marshal([]*tgbot.Subscription{})
	if err := os.WriteFile(jsonFile, emptyJSON, 0644); err != nil {
		log.Error().Err(err).Str("file", jsonFile).Msg("storage: failed to clear subscriptions JSON file after sync")
	}
}

// LoadSubscriptions loads subscriptions from JSON and/or SQL.
func (s *Storage) LoadSubscriptions() []*tgbot.Subscription {
	var subs []*tgbot.Subscription
	subsMap := make(map[int64]*tgbot.Subscription)

	s.mu.RLock()
	hasJSON := s.cfg.Database.HasUse("json")
	jsonFile := s.cfg.TgBot.File.Json
	db := s.db
	dbConnected := s.dbConnected
	s.mu.RUnlock()

	// 1. Load from JSON if enabled
	if hasJSON && jsonFile != "" {
		s.tgbotFileMu.RLock()
		data, err := os.ReadFile(jsonFile)
		s.tgbotFileMu.RUnlock()

		if err == nil {
			var list []*tgbot.Subscription
			if err := json.Unmarshal(data, &list); err == nil {
				for _, sub := range list {
					if sub.Settings != nil {
						sub.Settings.Validate()
					}
					subsMap[sub.ChatID] = sub
				}
			}
		}
	}

	// 2. Load from SQL if connected
	if dbConnected && db != nil {
		rows, err := db.Query("SELECT chat_id, data FROM bot_subscriptions")
		if err == nil {
			for rows.Next() {
				var chatID int64
				var data []byte
				if err := rows.Scan(&chatID, &data); err == nil {
					var sub tgbot.Subscription
					if err := json.Unmarshal(data, &sub); err == nil {
						if sub.Settings != nil {
							sub.Settings.Validate()
						}
						subsMap[chatID] = &sub
					}
				}
			}
			rows.Close()
		}
	}

	for _, sub := range subsMap {
		subs = append(subs, sub)
	}

	return subs
}

// SaveSubscription saves a single subscription (SQL) or updates the full JSON file based on dual-mode configuration.
func (s *Storage) SaveSubscription(sub *tgbot.Subscription, allSubs []*tgbot.Subscription) {
	s.mu.RLock()
	hasSQL := s.cfg.Database.DBProvider() != ""
	hasJSON := s.cfg.Database.HasUse("json")
	dbConnected := s.dbConnected && s.db != nil
	db := s.db
	jsonFile := s.cfg.TgBot.File.Json
	s.mu.RUnlock()

	saveJSON := func() {
		s.tgbotFileMu.Lock()
		defer s.tgbotFileMu.Unlock()
		if jsonFile == "" {
			return
		}
		jsonData, err := json.MarshalIndent(allSubs, "", "  ")
		if err != nil {
			return
		}
		_ = os.WriteFile(jsonFile, jsonData, 0644)
	}

	saveSQL := func() {
		if db == nil || sub == nil {
			return
		}
		subData, _ := json.Marshal(sub)
		if subData != nil {
			_, err := db.Exec("INSERT INTO bot_subscriptions (chat_id, data) VALUES ($1, $2) ON CONFLICT (chat_id) DO UPDATE SET data = $2", sub.ChatID, subData)
			if err != nil {
				log.Error().Err(err).Int64("chat_id", sub.ChatID).Msg("storage: failed to sync user to SQL")
			}
		}
	}

	if !hasSQL && hasJSON {
		saveJSON()
	} else if hasSQL && !hasJSON {
		saveSQL()
	} else if hasSQL && hasJSON {
		if dbConnected {
			saveSQL()
		} else {
			saveJSON()
		}
	}
}
