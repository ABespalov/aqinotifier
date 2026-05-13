package tgbot

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"
)

func (b *Bot) cmdSubscribeDevice(chatID int64, msg *telego.Message) {
	b.clearLastPrompt(chatID)
	input := strings.TrimSpace(msg.Text)

	if strings.HasPrefix(input, "/subscribe") {
		parts := strings.SplitN(input, " ", 2)
		if len(parts) == 2 {
			input = parts[1]
		} else {
			input = ""
		}
	}
	deviceID := strings.TrimSpace(input)
	if deviceID == "" {
		b.promptDeviceID(chatID)
		return
	}

	for _, c := range deviceID {
		if c < '0' || c > '9' {
			b.sendWithKeyboard(chatID, b.T(chatID, msgInvalidDeviceId), b.mainKeyboard(chatID))
			return
		}
	}

	var text string
	if b.store.Subscribe(chatID, deviceID, b.defaults) {
		text = b.TDevice(chatID, msgSubscribed, deviceID)
	} else {
		text = b.TDevice(chatID, msgAlreadySub, deviceID)
	}
	b.sendWithKeyboard(chatID, text, nil)
	b.cmdList(chatID)
}

func (b *Bot) cmdSettings(chatID int64) {
	b.clearLastPrompt(chatID)
	b.sendWithKeyboard(chatID, b.T(chatID, msgSettingsTitle), b.settingsKeyboard(chatID))
}

func (b *Bot) cmdStatusMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, msgNoSubs), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, devices[0]), b.mainKeyboard(chatID, devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", b.I(kIcoStatus), id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData(cmdHelp),
	})

	b.sendWithKeyboard(chatID, b.T(chatID, msgSelectDevice), tu.InlineKeyboard(rows...))
}

func (b *Bot) cmdList(chatID int64) {
	log.Debug().Int64("chat_id", chatID).Msg("tgbot: cmdList called")
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)

	var text string
	if len(devices) == 0 {
		text = b.T(chatID, msgNoSubs)
	} else {
		text = b.T(chatID, msgYourSubs)
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", b.I(kIcoDevice), b.formatDeviceIDPlain(chatID, id))).WithCallbackData(fmt.Sprintf("dev_settings:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnSubscribe)).WithCallbackData(cmdSubscribe),
		tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
	})

	b.sendWithKeyboard(chatID, text, tu.InlineKeyboard(rows...))
}

func (b *Bot) cmdUnsubscribeMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, msgNoSubs), b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", b.I(kIcoDelete), b.formatDeviceIDPlain(chatID, id))).WithCallbackData(fmt.Sprintf("unsub:%s", id)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
	})

	b.sendWithKeyboard(chatID, b.T(chatID, msgSelectUnsub), tu.InlineKeyboard(rows...))
}

func (b *Bot) cmdChartsMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, msgNoSubs), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.T(chatID, msgChartsMenu), b.chartsMenuKeyboard(chatID, devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", b.I(kIcoTrendUp), id)).WithCallbackData(fmt.Sprintf("charts_dev:%s", id)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
	})

	b.sendWithKeyboard(chatID, b.T(chatID, msgSelectDevice), tu.InlineKeyboard(rows...))
}

func (b *Bot) cmdLangMenu(chatID int64, editMsgID ...int) {
	if len(editMsgID) == 0 {
		b.clearLastPrompt(chatID)
	}
	
	current := b.store.GetLanguage(chatID)
	langLabel := b.T(chatID, "lang"+strings.Title(current))
	
	unitT := b.store.GetUnitTemp(chatID)
	unitP := b.store.GetUnitPress(chatID)
	
	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(langLabel).WithCallbackData("lang_cycle"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "txtUnit"+strings.Title(unitT))).WithCallbackData("unit_set:temp:toggle"),
			tu.InlineKeyboardButton(b.T(chatID, "txtUnit"+strings.Title(unitP))).WithCallbackData("unit_set:press:toggle"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)

	text := b.T(chatID, msgSelectLang)
	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		b.sendWithKeyboard(chatID, text, kb)
	}
}

func (b *Bot) cmdAqiMenu(chatID int64, editMsgID ...int) {
	mcfg := b.GetUserSettings(chatID)
	stdTag := strings.ToUpper(mcfg.AQIStandard)
	std, ok := sensor.Standards[stdTag]

	var details string
	if ok {
		stdName := std.NameFull
		stdLocKey := "standard" + strings.Title(strings.ToLower(stdTag))
		if loc := b.T(chatID, stdLocKey); !strings.HasPrefix(loc, "!!") {
			stdName = loc
		}

		title := b.T(chatID, "txtAqiStandardTitle", map[string]interface{}{
			"std":  stdName,
			"flag": b.Resolve(std.Flag),
		})

		var zones []string
		for _, z := range std.Zones {
			var aqiVal float64
			if z.Level < len(std.IndexPoints) {
				aqiVal = std.IndexPoints[z.Level]
			}

			name := z.Name
			key := fmt.Sprintf("aqiNameL%d%s", z.Level, strings.Title(strings.ToLower(stdTag)))
			if loc := b.T(chatID, key); !strings.HasPrefix(loc, "!!") {
				name = loc
			}

			zones = append(zones, b.T(chatID, "txtAqiStandardZone", map[string]interface{}{
				"ico":  b.Resolve(z.Icon),
				"name": name,
				"bp":   aqiVal,
			}))
		}
		details = title + "\n" + strings.Join(zones, "\n")
	}

	text := b.T(chatID, msgAqiSettings) + "\n\n" + details
	kb := b.aqiSettingsKeyboard(chatID)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		b.sendWithKeyboard(chatID, text, kb)
	}
}

func (b *Bot) cmdThresholdsMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	mcfg := b.GetUserSettings(chatID)
	text := b.T(chatID, msgThresholdsMenu, map[string]interface{}{
		"l1_25": mcfg.PM25L1, "l2_25": mcfg.PM25L2, "dyn25": mcfg.PM25Diff,
		"l1_10": mcfg.PM10L1, "l2_10": mcfg.PM10L2, "dyn10": mcfg.PM10Diff,
		"labelPm25": b.T(chatID, "labelPm25"), "labelPm10": b.T(chatID, "labelPm10"),
		"labelL1": b.T(chatID, "labelL1"), "labelL2": b.T(chatID, "labelL2"),
		"labelDynamics": b.T(chatID, "labelDynamics"),
	})

	b.sendWithKeyboard(chatID, text, b.thresholdsKeyboard(chatID))
}

func (b *Bot) cmdAQICycleMenu(chatID int64, editMsgID int, suggestedTags ...map[string]string) {
	sTags := make(map[string]string)
	if len(suggestedTags) > 0 {
		sTags = suggestedTags[0]
	}
	mcfg := b.GetUserSettings(chatID)
	std := strings.ToUpper(mcfg.AQIStandard)
	if _, ok := sensor.Standards[std]; !ok {
		return
	}

	text := b.T(chatID, msgAqiCycleMenu, map[string]interface{}{
		"l1_25": mcfg.PM25L1, "l2_25": mcfg.PM25L2,
		"l1_10": mcfg.PM10L1, "l2_10": mcfg.PM10L2,
	})

	getIcon := func(pmType string, val float64) (string, string, string) {
		activeTag := strings.ToUpper(mcfg.AQIStandard)
		
		// Check active standard first
		if stdData, ok := sensor.Standards[activeTag]; ok {
			var bp []float64
			if pmType == "PM10" {
				bp = stdData.Breakpoints10
			} else {
				bp = stdData.Breakpoints25
			}
			for _, v := range bp {
				if math.Abs(v-val) < 0.0001 {
					_, level := sensor.CalculateValueAQI(val, pmType, activeTag)
					return activeTag, b.Resolve(stdData.Flag), b.getAQIIcon(level, activeTag)
				}
			}
		}

		// Get other tags in sorted order
		var otherTags []string
		for tag := range sensor.Standards {
			if tag != activeTag {
				otherTags = append(otherTags, tag)
			}
		}
		sort.Strings(otherTags)

		// Check others
		for _, tag := range otherTags {
			stdData := sensor.Standards[tag]
			var bp []float64
			if pmType == "PM10" {
				bp = stdData.Breakpoints10
			} else {
				bp = stdData.Breakpoints25
			}
			for _, v := range bp {
				if math.Abs(v-val) < 0.0001 {
					_, level := sensor.CalculateValueAQI(val, pmType, tag)
					return tag, b.Resolve(stdData.Flag), b.getAQIIcon(level, tag)
				}
			}
		}
		return "", b.I(kIcoWrite), ""
	}

	btn := func(pmType, levelKey, zoneIcon string, val float64) telego.InlineKeyboardButton {
		key := pmType + ":" + levelKey
		var tag, flag, levelIcon string
		if sTag, ok := sTags[key]; ok {
			tag = sTag
			if stdData, ok := sensor.Standards[tag]; ok {
				flag = stdData.Flag
				_, level := sensor.CalculateValueAQI(val, pmType, tag)
				levelIcon = b.getAQIIcon(level, tag)
			}
		}
		
		if tag == "" {
			tag, flag, levelIcon = getIcon(pmType, val)
		}
		
		label := fmt.Sprintf("%s%s ⇐ %s%s %.1f", pmType, zoneIcon, flag, levelIcon, val)
		if levelIcon == "" {
			label = fmt.Sprintf("%s%s ⇐ %s %.1f", pmType, zoneIcon, flag, val)
		}
		return tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("aqi_cycle:%s:%s:%s", pmType, levelKey, tag))
	}

	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			btn("PM25", "level1", b.I(kIcoPmLevel1), mcfg.PM25L1),
			btn("PM10", "level1", b.I(kIcoPmLevel1), mcfg.PM10L1),
		),
		tu.InlineKeyboardRow(
			btn("PM25", "level2", b.I(kIcoPmLevel2), mcfg.PM25L2),
			btn("PM10", "level2", b.I(kIcoPmLevel2), mcfg.PM10L2),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnAqiBackToThresholds)).WithCallbackData(cmdThresholdsMenu),
		),
	)

	if editMsgID > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID, text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		b.sendWithKeyboard(chatID, text, kb)
	}
}

func (b *Bot) cmdHistoryMenu(chatID int64) {
	log.Debug().Int64("chat_id", chatID).Msg("tgbot: cmdHistoryMenu called")
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, msgNoSubs), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.cmdDeviceHistory(chatID, devices[0])
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", b.I(kIcoHistory), id)).WithCallbackData(fmt.Sprintf("history:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
	})

	b.sendWithKeyboard(chatID, b.T(chatID, msgSelectHistory), tu.InlineKeyboard(rows...))
}

func (b *Bot) cmdSoundMenu(chatID int64, silent bool, editMsgID ...int) {
	var templateKey string
	if silent {
		templateKey = msgSilentAlerts
	} else {
		templateKey = msgLoudAlerts
	}
	text := b.T(chatID, templateKey) + b.T(chatID, msgSoundSettings)

	kb := b.notificationSettingsKeyboard(chatID, silent)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		b.clearLastPrompt(chatID)
		b.sendWithKeyboard(chatID, text, kb)
	}
}

func (b *Bot) cmdRename(chatID int64, deviceID string) {
	b.setState(chatID, stateAwaitDeviceName)
	b.renameIDMu.Lock()
	b.renameIDs[chatID] = deviceID
	b.renameIDMu.Unlock()

	text := b.TDevice(chatID, msgRenamePrompt, deviceID)
	keyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, msgRenameCancel)).WithCallbackData(fmt.Sprintf("rename_cancel:%s", deviceID)),
		),
	)
	b.sendWithKeyboard(chatID, text, keyboard)
}

func (b *Bot) cmdDeviceHistory(chatID int64, deviceID string) {
	b.clearLastPrompt(chatID)
	log.Debug().Int64("chat_id", chatID).Str("device_id", deviceID).Msg("tgbot: cmdDeviceHistory start")

	_ = b.api.SendChatAction(context.Background(), &telego.SendChatActionParams{ChatID: tu.ID(chatID), Action: "upload_photo"})

	history := b.monitor.GetHistory(deviceID)
	log.Debug().Int64("chat_id", chatID).Int("count", len(history)).Msg("tgbot: history loaded")

	if len(history) == 0 {
		b.sendWithKeyboard(chatID, b.TDevice(chatID, msgHistoryEmpty, deviceID), b.mainKeyboard(chatID))
		return
	}

	log.Debug().Int64("chat_id", chatID).Msg("tgbot: drawing charts start")
	buffers, err := generateCharts(b, chatID, history, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize, chartSmoothingHistory)
	if err != nil {
		log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to generate charts")
		b.sendWithKeyboard(chatID, b.T(chatID, msgHistoryError), b.mainKeyboard(chatID))
		return
	}
	log.Debug().Int64("chat_id", chatID).Int("charts", len(buffers)).Msg("tgbot: drawing charts end")

	var media []telego.InputMedia
	for i, buf := range buffers {
		nr := &bytesNamedReader{
			Reader: bytes.NewReader(buf),
			name:   fmt.Sprintf("chart_%d.png", i),
		}

		photo := &telego.InputMediaPhoto{
			Type:  "photo",
			Media: tu.File(nr),
		}
		media = append(media, photo)
	}

	params := tu.MediaGroup(tu.ID(chatID), media...)
	msgs, err := b.api.SendMediaGroup(context.Background(), params)
	if err == nil {
		for _, m := range msgs {
			b.setLastPrompt(chatID, m.GetMessageID())
		}
	} else {
		log.Error().Err(err).Int64("chat_id", chatID).Msg("tgbot: failed to send media group")
		// Fallback to individual photos if media group fails
		for i, buf := range buffers {
			nr := &bytesNamedReader{Reader: bytes.NewReader(buf), name: fmt.Sprintf("chart_%d.png", i)}
			p := &telego.SendPhotoParams{ChatID: tu.ID(chatID), Photo: tu.File(nr)}
			m, err := b.api.SendPhoto(context.Background(), p)
			if err == nil {
				b.setLastPrompt(chatID, m.GetMessageID())
			}
		}
	}

	// Send footer with keyboard
	mcfg := b.GetUserSettings(chatID)
	deviceName := mcfg.DeviceNames[deviceID]
	if deviceName == "" {
		deviceName = deviceID
	}
	footer := b.T(chatID, msgHistoryFooter, map[string]interface{}{
		"count": len(history), "deviceId": deviceID, "deviceName": deviceName,
	})
	b.sendWithKeyboard(chatID, footer, b.mainKeyboard(chatID))
}

func (b *Bot) cmdResetConfirm(chatID int64) {
	b.clearLastPrompt(chatID)
	d := b.defaults
	std := strings.ToLower(d.AQIStandard)
	stdLabel := b.T(chatID, "standard"+strings.Title(std))

	unitT := b.T(chatID, "txtUnit"+strings.Title(strings.ToLower(b.cfg.DefaultUnitTemp)))
	unitP := b.T(chatID, "txtUnit"+strings.Title(strings.ToLower(b.cfg.DefaultUnitPress)))

	var alertsSB strings.Builder
	allAlerts := b.getAllAlerts(chatID, "")

	defNotifications := make(map[string]bool)
	for _, n := range d.Notifications {
		defNotifications[n] = true
	}
	defWarnings := make(map[string]bool)
	for _, w := range d.Warnings {
		defWarnings[w] = true
	}

	for _, a := range allAlerts {
		if defNotifications[a.id] {
			statusIcon := b.I(kIcoSilent)
			if defWarnings[a.id] {
				statusIcon = b.I(kIcoLoud)
			}
			atoms := map[string]interface{}{
				"statusIcon": statusIcon,
				"pm":         a.pm,
				"action":     a.action,
				"icon":       a.actionIcon,
				"zone":       a.zone,
				"zoneIcon":   a.zoneIcon,
				"in":         b.T(chatID, "txtLabelIn"),
				"delta":      a.delta,
				"aqiPrefix":  a.aqiPrefix,
				"aqiName":    a.aqiName,
				"isAqi":      strings.HasPrefix(a.id, "aqi_"),
				"isVal":      strings.HasPrefix(a.id, "val"),
				"isDiff":     strings.HasPrefix(a.id, "diff"),
			}

			if atoms["isAqi"].(bool) {
				atoms["name"] = fmt.Sprintf("%s: %s", a.aqiPrefix, a.aqiName)
			} else if atoms["isVal"].(bool) {
				atoms["name"] = b.T(chatID, "alertValBtn", atoms)
			} else {
				atoms["name"] = b.T(chatID, "alertDiffBtn", atoms)
			}

			line := b.T(chatID, "msgResetAlertItem", atoms)
			alertsSB.WriteString(line + "\n")
		}
	}

	text := b.T(chatID, "msgResetConfirm", map[string]interface{}{
		"l1_25": d.PM25L1, "l2_25": d.PM25L2, "dyn25": d.PM25Diff,
		"l1_10": d.PM10L1, "l2_10": d.PM10L2, "dyn10": d.PM10Diff,
		"stdName": stdLabel, "aqiStandardFlag": b.I("icoFlag" + strings.ToUpper(std)),
		"unitT": unitT, "unitP": unitP,
		"alertsList": alertsSB.String(),
	})

	b.sendWithKeyboard(chatID, text, b.resetDefaultsKeyboard(chatID))
}

func (b *Bot) cmdResetExecute(chatID int64) {
	b.store.ResetSettings(chatID, b.defaults)

	mcfg := b.store.GetSettings(chatID, b.defaults)
	text := b.T(chatID, msgResetExecution, map[string]interface{}{
		"l1_25": mcfg.PM25L1, "l2_25": mcfg.PM25L2, "dyn25": mcfg.PM25Diff,
		"l1_10": mcfg.PM10L1, "l2_10": mcfg.PM10L2, "dyn10": mcfg.PM10Diff,
	})

	b.sendWithKeyboard(chatID, text, b.settingsKeyboard(chatID))
}
