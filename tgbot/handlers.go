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
		_, isNew := b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		b.sendHelp(chatID)

		if isNew && len(b.store.Subscriptions(chatID)) == 0 {
			b.promptDeviceID(chatID)
		}

		return nil
	}, th.CommandEqual("start"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.setState(chatID, stateIdle)
		b.sendHelp(chatID)
		return nil
	}, th.CommandEqual("help"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdList(chatID)
		return nil
	}, th.CommandEqual("list"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdStatusMenu(chatID)
		return nil
	}, th.CommandEqual("status"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdUnsubscribeMenu(chatID)
		return nil
	}, th.CommandEqual("unsubscribe"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		text := update.Message.Text
		parts := strings.SplitN(text, " ", 2)
		if len(parts) < 2 {
			b.promptDeviceID(chatID)
			return nil
		}
		b.cmdSubscribeDevice(chatID, update.Message)
		return nil
	}, th.CommandEqual("subscribe"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		b.syncUser(update.Message.From, chatID)
		b.cmdLangMenu(chatID)
		return nil
	}, th.CommandEqual("lang"))

	b.handler.Handle(func(_ *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		changed, _ := b.syncUser(update.Message.From, chatID)
		if changed {
			log.Debug().Int64("chat_id", chatID).Msg("tgbot: language changed, refreshing menu")

			b.sendWithKeyboard(chatID, b.T(chatID, "msgHelp"), b.mainKeyboard(chatID))
			return nil
		}
		b.handleMessage(update.Message)
		return nil
	}, th.AnyMessageWithText())

	b.handler.HandleCallbackQuery(func(_ *th.Context, query telego.CallbackQuery) error {
		b.syncUser(&query.From, query.Message.GetChat().ID)
		b.handleCallback(&query)
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
	text := strings.TrimSpace(msg.Text)

	state := b.getState(chatID)
	log.Debug().Int64("chat_id", chatID).Int("state", int(state)).Str("text", text).Msg("tgbot: handleMessage")

	switch state {
	case stateAwaitDeviceID:
		b.setState(chatID, stateIdle)
		b.cmdSubscribeDevice(chatID, msg)
		return
	case stateAwaitPM10Level1:
		b.handleThresholdUpdate(chatID, "PM10", "level1", text)
	case stateAwaitPM10Level2:
		b.handleThresholdUpdate(chatID, "PM10", "level2", text)
	case stateAwaitPM25Level1:
		b.handleThresholdUpdate(chatID, "PM2.5", "level1", text)
	case stateAwaitPM25Level2:
		b.handleThresholdUpdate(chatID, "PM2.5", "level2", text)
	case stateAwaitDiff10:
		b.handleThresholdUpdate(chatID, "PM10", "diff", text)
	case stateAwaitDiff25:
		b.handleThresholdUpdate(chatID, "PM2.5", "diff", text)
	case stateAwaitDeviceName:
		b.handleDeviceRename(chatID, msg)
		return
	default:
		return
	}
}

func (b *Bot) handleThresholdUpdate(chatID int64, pmType, level, text string) {
	var val float64
	_, err := fmt.Sscanf(text, "%f", &val)
	if err != nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorNumber"), nil)
		return
	}
	if val < 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msgErrorPositive"), nil)
		return
	}

	mcfg := b.GetUserSettings(chatID)
	var old float64
	titleKey := "msgThresholdTitleFmt"

	switch pmType {
	case "PM10":
		switch level {
		case "level1":
			old = mcfg.PM10L1
			mcfg.PM10L1 = val
		case "level2":
			old = mcfg.PM10L2
			mcfg.PM10L2 = val
		case "diff":
			old = mcfg.PM10Diff
			mcfg.PM10Diff = val
		}
	case "PM2.5":
		switch level {
		case "level1":
			old = mcfg.PM25L1
			mcfg.PM25L1 = val
		case "level2":
			old = mcfg.PM25L2
			mcfg.PM25L2 = val
		case "diff":
			old = mcfg.PM25Diff
			mcfg.PM25Diff = val
		}
	}

	b.store.UpdateSettings(chatID, mcfg)
	b.setState(chatID, stateIdle)

	res := b.T(chatID, "msgThresholdUpd", map[string]interface{}{
		"title": b.T(chatID, titleKey, map[string]interface{}{
			"pm": pmType, "label": b.T(chatID, "labelThreshold"),
		}),
		"old": old, "new": val,
	})
	b.clearLastPrompt(chatID)
	b.sendWithKeyboard(chatID, res, b.thresholdsKeyboard(chatID))
}

func (b *Bot) handleDeviceRename(chatID int64, msg *telego.Message) {
	b.renameIDMu.Lock()
	deviceID, ok := b.renameIDs[chatID]
	b.renameIDMu.Unlock()
	if !ok {
		b.setState(chatID, stateIdle)
		return
	}

	name := strings.TrimSpace(msg.Text)
	if name == "" {
		return
	}

	mcfg := b.GetUserSettings(chatID)
	if mcfg.DeviceNames == nil {
		mcfg.DeviceNames = make(map[string]string)
	}
	mcfg.DeviceNames[deviceID] = name
	b.store.UpdateSettings(chatID, mcfg)

	b.setState(chatID, stateIdle)
	b.renameIDMu.Lock()
	delete(b.renameIDs, chatID)
	b.renameIDMu.Unlock()

	b.sendWithKeyboard(chatID, b.T(chatID, "msgDeviceRenamed", map[string]interface{}{
		"name": name, "txtDevice": deviceID,
	}), nil)
	b.cmdList(chatID)
}

func (b *Bot) promptDeviceID(chatID int64) {
	b.setState(chatID, stateAwaitDeviceID)
	b.sendWithKeyboard(chatID, b.T(chatID, "msgPromptDevice"), b.cancelSubKeyboard(chatID))
}

func (b *Bot) setLastPrompt(chatID int64, msgID int) {
	b.store.AddLastPrompt(chatID, msgID)
}

func (b *Bot) promptThreshold(chatID int64, param, levelKey string) {
	mcfg := b.GetUserSettings(chatID)
	var currentVal float64

	var pmLabel string
	var zoneLabel string
	var zoneIcon string

	switch param {
	case "PM10":
		pmLabel = b.T(chatID, "labelPm10")
		switch levelKey {
		case "level1":
			b.setState(chatID, stateAwaitPM10Level1)
			zoneLabel = b.T(chatID, "labelZoneGreen")
			zoneIcon = b.I(kIcoPmLevel1)
			currentVal = mcfg.PM10L1
		case "level2":
			b.setState(chatID, stateAwaitPM10Level2)
			zoneLabel = b.T(chatID, "labelZoneYellow")
			zoneIcon = b.I(kIcoPmLevel2)
			currentVal = mcfg.PM10L2
		case "diff":
			b.setState(chatID, stateAwaitDiff10)
			zoneLabel = b.T(chatID, "msgThresholdDiffTitle")
			zoneIcon = b.I(kIcoPm10)
			currentVal = mcfg.PM10Diff
		}
	case "PM2.5":
		pmLabel = b.T(chatID, "labelPm25")
		switch levelKey {
		case "level1":
			b.setState(chatID, stateAwaitPM25Level1)
			zoneLabel = b.T(chatID, "labelZoneGreen")
			zoneIcon = b.I(kIcoPmLevel1)
			currentVal = mcfg.PM25L1
		case "level2":
			b.setState(chatID, stateAwaitPM25Level2)
			zoneLabel = b.T(chatID, "labelZoneYellow")
			zoneIcon = b.I(kIcoPmLevel2)
			currentVal = mcfg.PM25L2
		case "diff":
			b.setState(chatID, stateAwaitDiff25)
			zoneLabel = b.T(chatID, "msgThresholdDiffTitle")
			zoneIcon = b.I(kIcoPm25)
			currentVal = mcfg.PM25Diff
		}
	}

	title := b.T(chatID, "msgThresholdTitleFmt", map[string]interface{}{
		"pm": pmLabel, "label": b.T(chatID, "labelThreshold"), "zoneStr": zoneLabel, "zoneIcon": zoneIcon, "suffix": "",
	})

	text := b.T(chatID, "msgThresholdPrompt", map[string]interface{}{
		"title": title, "curr": currentVal,
	})

	b.sendWithKeyboard(chatID, text, b.cancelThresholdKeyboard(chatID))
}
