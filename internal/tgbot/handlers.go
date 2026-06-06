// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file registers Telegram message and callback query handlers, maintains
// the user conversation state machine, and processes text inputs.
package tgbot

import (
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	"github.com/rs/zerolog/log"
)

func (b *Bot) syncUser(u *telego.User, chatID int64) (changed bool, isNew bool) {
	if u == nil {
		log.Warn().Int64("chat_id", chatID).Msg("tgbot: syncUser failed because User is nil")
		return false, false
	}
	detected := b.detectLang(u.LanguageCode)
	changed, isNew = b.store.SyncLanguage(chatID, u.LanguageCode, detected)
	if changed {
		log.Info().Int64("chat_id", chatID).Str("new", detected).Str("tg_code", u.LanguageCode).Msg("tgbot: language automatically synced from Telegram")
		b.updateCommandsForUser(chatID, detected)
	}
	return changed, isNew
}
func (b *Bot) registerHandlers() {

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		_, isNew := b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		ctx.sendHelp()

		if isNew && len(b.store.Subscriptions(chatID)) == 0 {
			ctx.promptDeviceID()
		}

		return nil
	}, th.CommandEqual("start"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		ctx.sendHelp()
		return nil
	}, th.CommandEqual("help"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		b.syncUser(update.Message.From, chatID)
		ctx.cmdList()
		return nil
	}, th.CommandEqual("list"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		b.syncUser(update.Message.From, chatID)
		ctx.cmdStatusMenu()
		return nil
	}, th.CommandEqual("status"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		b.syncUser(update.Message.From, chatID)
		ctx.cmdUnsubscribeMenu()
		return nil
	}, th.CommandEqual("unsubscribe"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		b.syncUser(update.Message.From, chatID)
		text := update.Message.Text
		parts := strings.SplitN(text, " ", 2)
		if len(parts) < 2 {
			ctx.promptDeviceID()
			return nil
		}
		ctx.cmdSubscribeDevice(update.Message)
		return nil
	}, th.CommandEqual("subscribe"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		b.syncUser(update.Message.From, chatID)
		ctx.cmdLangMenu()
		return nil
	}, th.CommandEqual("lang"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		ctx := b.NewContext(chatID)
		changed, _ := b.syncUser(update.Message.From, chatID)
		if changed {
			log.Debug().Int64("chat_id", chatID).Msg("tgbot: language changed, refreshing menu")

			ctx.sendWithKeyboard(ctx.T("msgHelp"), ctx.mainKeyboard())
			return nil
		}
		b.handleMessage(update.Message)
		return nil
	}, th.AnyMessageWithText())

	b.handler.HandleCallbackQuery(func(_ *th.Context, query telego.CallbackQuery) error {
		b.syncUser(&query.From, query.Message.GetChat().ID)
		ctx := b.NewContext(query.Message.GetChat().ID)
		ctx.handleCallback(&query)
		return nil
	}, th.AnyCallbackQuery())
}

func (b *Bot) getState(chatID int64) chatState {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	return b.states[chatID]
}

func (b *Bot) setState(chatID int64, s chatState) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	b.states[chatID] = s
}

func (b *Bot) handleMessage(msg *telego.Message) {
	chatID := msg.Chat.ID
	ctx := b.NewContext(chatID)
	text := strings.TrimSpace(msg.Text)

	state := b.getState(chatID)
	log.Debug().Int64("chat_id", chatID).Int("state", int(state)).Str("text", text).Msg("tgbot: handleMessage")

	switch state {
	case stateAwaitDeviceID:
		b.setState(chatID, stateIdle)
		ctx.cmdSubscribeDevice(msg)
		return
	case stateAwaitPM10Level1:
		ctx.handleThresholdUpdate("PM10", "level1", text)
	case stateAwaitPM10Level2:
		ctx.handleThresholdUpdate("PM10", "level2", text)
	case stateAwaitPM25Level1:
		ctx.handleThresholdUpdate("PM2.5", "level1", text)
	case stateAwaitPM25Level2:
		ctx.handleThresholdUpdate("PM2.5", "level2", text)
	case stateAwaitDiff10:
		ctx.handleThresholdUpdate("PM10", "diff", text)
	case stateAwaitDiff25:
		ctx.handleThresholdUpdate("PM2.5", "diff", text)
	case stateAwaitAQILazyUp:
		ctx.handleLazyUpdate("aqi", "up", text)
	case stateAwaitAQILazyDown:
		ctx.handleLazyUpdate("aqi", "down", text)
	case stateAwaitPM10LazyUp:
		ctx.handleLazyUpdate("pm10", "up", text)
	case stateAwaitPM10LazyDown:
		ctx.handleLazyUpdate("pm10", "down", text)
	case stateAwaitPM25LazyUp:
		ctx.handleLazyUpdate("pm25", "up", text)
	case stateAwaitPM25LazyDown:
		ctx.handleLazyUpdate("pm25", "down", text)
	case stateAwaitDeviceName:
		ctx.handleDeviceRename(msg)
		return
	default:
		return
	}
}

func (ctx *RequestContext) handleThresholdUpdate(pmType, level, text string) {
	var val float64
	_, err := fmt.Sscanf(text, "%f", &val)
	if err != nil {
		ctx.sendWithKeyboard(ctx.T("msgErrorNumber"), nil)
		return
	}
	if val < 0 {
		ctx.sendWithKeyboard(ctx.T("msgErrorPositive"), nil)
		return
	}

	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	var old float64
	titleKey := "msgThresholdTitleFmt"

	switch pmType {
	case "PM10":
		switch level {
		case "level1":
			old = mcfg.PM10.Level1
			mcfg.PM10.Level1 = val
		case "level2":
			old = mcfg.PM10.Level2
			mcfg.PM10.Level2 = val
		case "diff":
			old = mcfg.PM10.Diff
			mcfg.PM10.Diff = val
		}
	case "PM2.5":
		switch level {
		case "level1":
			old = mcfg.PM25.Level1
			mcfg.PM25.Level1 = val
		case "level2":
			old = mcfg.PM25.Level2
			mcfg.PM25.Level2 = val
		case "diff":
			old = mcfg.PM25.Diff
			mcfg.PM25.Diff = val
		}
	}

	ctx.Bot.store.UpdateSettings(ctx.ChatID, mcfg)
	ctx.Bot.setState(ctx.ChatID, stateIdle)

	res := ctx.T("msgThresholdUpd", map[string]interface{}{
		"title": ctx.T(titleKey, map[string]interface{}{
			"pm": pmType, "label": ctx.T("labelThreshold"),
		}),
		"old": old, "new": val,
	})
	ctx.clearLastPrompt()
	ctx.sendWithKeyboard(res, ctx.thresholdsKeyboard())
}

func (ctx *RequestContext) handleDeviceRename(msg *telego.Message) {
	ctx.Bot.renameIDMu.Lock()
	deviceID, ok := ctx.Bot.renameIDs[ctx.ChatID]
	ctx.Bot.renameIDMu.Unlock()
	if !ok {
		ctx.Bot.setState(ctx.ChatID, stateIdle)
		return
	}

	name := strings.TrimSpace(msg.Text)
	if name == "" {
		return
	}

	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	if mcfg.DeviceNames == nil {
		mcfg.DeviceNames = make(map[string]string)
	}
	mcfg.DeviceNames[deviceID] = name
	ctx.Bot.store.UpdateSettings(ctx.ChatID, mcfg)

	ctx.Bot.setState(ctx.ChatID, stateIdle)
	ctx.Bot.renameIDMu.Lock()
	delete(ctx.Bot.renameIDs, ctx.ChatID)
	ctx.Bot.renameIDMu.Unlock()

	ctx.sendWithKeyboard(ctx.T("msgDeviceRenamed", map[string]interface{}{
		"deviceId":   deviceID,
		"deviceName": name,
	}), nil)
	ctx.cmdList()
}

func (ctx *RequestContext) promptDeviceID() {
	ctx.Bot.setState(ctx.ChatID, stateAwaitDeviceID)
	ctx.sendWithKeyboard(ctx.T("msgPromptDevice"), ctx.cancelSubKeyboard())
}

func (ctx *RequestContext) setLastPrompt(msgID int) {
	ctx.Bot.store.AddLastPrompt(ctx.ChatID, msgID)
}

func (ctx *RequestContext) promptThreshold(param, levelKey string) {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	var currentVal float64

	var pmLabel string
	var zoneLabel string
	var zoneIcon string

	switch param {
	case "PM10":
		pmLabel = ctx.T("labelPm10")
		switch levelKey {
		case "level1":
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM10Level1)
			zoneLabel = ctx.T("labelZoneGreen")
			zoneIcon = ctx.Bot.TLang("en", kIcoPmLevel1)
			currentVal = mcfg.PM10.Level1
		case "level2":
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM10Level2)
			zoneLabel = ctx.T("labelZoneYellow")
			zoneIcon = ctx.Bot.TLang("en", kIcoPmLevel2)
			currentVal = mcfg.PM10.Level2
		case "diff":
			ctx.Bot.setState(ctx.ChatID, stateAwaitDiff10)
			zoneLabel = ctx.T("msgThresholdDiffTitle")
			zoneIcon = ctx.Bot.TLang("en", kIcoPm10)
			currentVal = mcfg.PM10.Diff
		}
	case "PM2.5":
		pmLabel = ctx.T("labelPm25")
		switch levelKey {
		case "level1":
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM25Level1)
			zoneLabel = ctx.T("labelZoneGreen")
			zoneIcon = ctx.Bot.TLang("en", kIcoPmLevel1)
			currentVal = mcfg.PM25.Level1
		case "level2":
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM25Level2)
			zoneLabel = ctx.T("labelZoneYellow")
			zoneIcon = ctx.Bot.TLang("en", kIcoPmLevel2)
			currentVal = mcfg.PM25.Level2
		case "diff":
			ctx.Bot.setState(ctx.ChatID, stateAwaitDiff25)
			zoneLabel = ctx.T("msgThresholdDiffTitle")
			zoneIcon = ctx.Bot.TLang("en", kIcoPm25)
			currentVal = mcfg.PM25.Diff
		}
	}

	title := ctx.T("msgThresholdTitleFmt", map[string]interface{}{
		"pm": pmLabel, "label": ctx.T("labelThreshold"), "zoneStr": zoneLabel, "zoneIcon": zoneIcon, "suffix": "",
	})

	text := ctx.T("msgThresholdPrompt", map[string]interface{}{
		"title": title, "curr": currentVal,
	})

	ctx.sendWithKeyboard(text, ctx.cancelThresholdKeyboard())
}

func (ctx *RequestContext) promptLazy(metric, direction string) {
	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	var currentVal int

	var metricLabel string
	switch metric {
	case "aqi":
		metricLabel = "AQI"
		if direction == "up" {
			ctx.Bot.setState(ctx.ChatID, stateAwaitAQILazyUp)
			if mcfg.AQI.LazyNotify.Up != nil {
				currentVal = *mcfg.AQI.LazyNotify.Up
			}
		} else {
			ctx.Bot.setState(ctx.ChatID, stateAwaitAQILazyDown)
			if mcfg.AQI.LazyNotify.Down != nil {
				currentVal = *mcfg.AQI.LazyNotify.Down
			}
		}
	case "pm10":
		metricLabel = "PM10"
		if direction == "up" {
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM10LazyUp)
			if mcfg.PM10.LazyNotify.Up != nil {
				currentVal = *mcfg.PM10.LazyNotify.Up
			}
		} else {
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM10LazyDown)
			if mcfg.PM10.LazyNotify.Down != nil {
				currentVal = *mcfg.PM10.LazyNotify.Down
			}
		}
	case "pm25":
		metricLabel = "PM2.5"
		if direction == "up" {
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM25LazyUp)
			if mcfg.PM25.LazyNotify.Up != nil {
				currentVal = *mcfg.PM25.LazyNotify.Up
			}
		} else {
			ctx.Bot.setState(ctx.ChatID, stateAwaitPM25LazyDown)
			if mcfg.PM25.LazyNotify.Down != nil {
				currentVal = *mcfg.PM25.LazyNotify.Down
			}
		}
	}

	title := ctx.T("msgLazyPromptTitle", map[string]interface{}{
		"metric": metricLabel,
		"dir":    direction,
	})

	text := ctx.T("msgLazyPrompt", map[string]interface{}{
		"title": title,
		"curr":  currentVal,
	})

	ctx.sendWithKeyboard(text, ctx.cancelLazyKeyboard())
}

func (ctx *RequestContext) handleLazyUpdate(metric, direction, text string) {
	var val int
	_, err := fmt.Sscanf(text, "%d", &val)
	if err != nil {
		ctx.sendWithKeyboard(ctx.T("msgErrorNumber"), nil)
		return
	}
	if val < 0 {
		ctx.sendWithKeyboard(ctx.T("msgErrorPositive"), nil)
		return
	}

	mcfg := ctx.Bot.GetUserSettings(ctx.ChatID)
	var old int

	var metricLabel string
	switch metric {
	case "aqi":
		metricLabel = "AQI"
		if direction == "up" {
			if mcfg.AQI.LazyNotify.Up != nil {
				old = *mcfg.AQI.LazyNotify.Up
			}
			mcfg.AQI.LazyNotify.Up = &val
		} else {
			if mcfg.AQI.LazyNotify.Down != nil {
				old = *mcfg.AQI.LazyNotify.Down
			}
			mcfg.AQI.LazyNotify.Down = &val
		}
	case "pm10":
		metricLabel = "PM10"
		if direction == "up" {
			if mcfg.PM10.LazyNotify.Up != nil {
				old = *mcfg.PM10.LazyNotify.Up
			}
			mcfg.PM10.LazyNotify.Up = &val
		} else {
			if mcfg.PM10.LazyNotify.Down != nil {
				old = *mcfg.PM10.LazyNotify.Down
			}
			mcfg.PM10.LazyNotify.Down = &val
		}
	case "pm25":
		metricLabel = "PM2.5"
		if direction == "up" {
			if mcfg.PM25.LazyNotify.Up != nil {
				old = *mcfg.PM25.LazyNotify.Up
			}
			mcfg.PM25.LazyNotify.Up = &val
		} else {
			if mcfg.PM25.LazyNotify.Down != nil {
				old = *mcfg.PM25.LazyNotify.Down
			}
			mcfg.PM25.LazyNotify.Down = &val
		}
	}

	ctx.Bot.store.UpdateSettings(ctx.ChatID, mcfg)
	ctx.Bot.setState(ctx.ChatID, stateIdle)

	title := ctx.T("msgLazyPromptTitle", map[string]interface{}{
		"metric": metricLabel,
		"dir":    direction,
	})

	res := ctx.T("msgLazyUpd", map[string]interface{}{
		"title": title,
		"dir":   direction,
		"old":   old,
		"new":   val,
	})
	ctx.clearLastPrompt()
	ctx.sendWithKeyboard(res, ctx.lazySettingsKeyboard())
}
