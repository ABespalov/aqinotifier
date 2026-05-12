package tgbot

import (
	"bytes"
	"context"
	"fmt"
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

func (b *Bot) cmdLangMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "langRu")).WithCallbackData("lang_set:ru"),
			tu.InlineKeyboardButton(b.T(chatID, "langEn")).WithCallbackData("lang_set:en"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "txtUnit"+strings.Title(b.store.GetUnitTemp(chatID)))).WithCallbackData("unit_set:temp:toggle"),
			tu.InlineKeyboardButton(b.T(chatID, "txtUnit"+strings.Title(b.store.GetUnitPress(chatID)))).WithCallbackData("unit_set:press:toggle"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnBackSettings)).WithCallbackData(cmdSettings),
		),
	)
	b.sendWithKeyboard(chatID, b.T(chatID, msgSelectLang), kb)
}

func (b *Bot) cmdAqiMenu(chatID int64, editMsgID ...int) {
	text := b.T(chatID, msgAqiSettings)
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
		"vg1": mcfg.PM25L1, "vy1": mcfg.PM25L2, "vdyn1": mcfg.PM25Diff,
		"vg2": mcfg.PM10L1, "vy2": mcfg.PM10L2, "vdyn2": mcfg.PM10Diff,
		"labelPm25": b.T(chatID, "labelPm25"), "labelPm10": b.T(chatID, "labelPm10"),
		"labelZoneGreen": b.T(chatID, "labelZoneGreen"), "labelZoneYellow": b.T(chatID, "labelZoneYellow"),
		"labelDynamics": b.T(chatID, "labelDynamics"),
	})

	b.sendWithKeyboard(chatID, text, b.thresholdsKeyboard(chatID))
}

func (b *Bot) cmdAQICycleMenu(chatID int64, editMsgID ...int) {
	mcfg := b.GetUserSettings(chatID)

	text := b.T(chatID, msgAqiCycleMenu, map[string]interface{}{
		"vg1": mcfg.PM25L1, "vy1": mcfg.PM25L2,
		"vg2": mcfg.PM10L1, "vy2": mcfg.PM10L2,
	})

	getIcon := func(pmType string, val float64) (string, string) {
		var eu, us []float64
		if pmType == "PM10" {
			eu, us = sensor.BreakpointsEU10, sensor.BreakpointsUS10
		} else {
			eu, us = sensor.BreakpointsEU25, sensor.BreakpointsUS25
		}

		active := strings.ToLower(mcfg.AQIStandard)
		var std string
		var flag string
		found := false

		if active == "eu" {
			for _, v := range eu {
				if v == val {
					std = "EU"
					flag = b.I(kIcoFlagEU)
					found = true
					break
				}
			}
			if !found {
				for _, v := range us {
					if v == val {
						std = "US"
						flag = b.I(kIcoFlagUS)
						found = true
						break
					}
				}
			}
		} else {
			for _, v := range us {
				if v == val {
					std = "US"
					flag = b.I(kIcoFlagUS)
					found = true
					break
				}
			}
			if !found {
				for _, v := range eu {
					if v == val {
						std = "EU"
						flag = b.I(kIcoFlagEU)
						found = true
						break
					}
				}
			}
		}

		if found {
			_, level := sensor.CalculateValueAQI(val, pmType, std)
			return flag, b.getAQIIcon(level, std)
		}
		return b.I(kIcoWrite), ""
	}

	btnText := func(pmType, zoneIcon string, val float64) string {
		flag, levelIcon := getIcon(pmType, val)
		if levelIcon != "" {
			return fmt.Sprintf("%s%s ⇐ %s%s %.1f", pmType, zoneIcon, flag, levelIcon, val)
		}
		return fmt.Sprintf("%s%s ⇐ %s %.1f", pmType, zoneIcon, flag, val)
	}

	kb := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnText(b.T(chatID, "labelPm25"), b.I(kIcoPmLevel1), mcfg.PM25L1)).WithCallbackData("aqi_cycle:PM2.5:level1"),
			tu.InlineKeyboardButton(btnText(b.T(chatID, "labelPm10"), b.I(kIcoPmLevel1), mcfg.PM10L1)).WithCallbackData("aqi_cycle:PM10:level1"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnText(b.T(chatID, "labelPm25"), b.I(kIcoPmLevel2), mcfg.PM25L2)).WithCallbackData("aqi_cycle:PM2.5:level2"),
			tu.InlineKeyboardButton(btnText(b.T(chatID, "labelPm10"), b.I(kIcoPmLevel2), mcfg.PM10L2)).WithCallbackData("aqi_cycle:PM10:level2"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnAqiBackToThresholds)).WithCallbackData(cmdThresholdsMenu),
		),
	)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
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
		"pm25G": d.PM25L1, "pm25Y": d.PM25L2, "pm25Dyn": d.PM25Diff,
		"pm10G": d.PM10L1, "pm10Y": d.PM10L2, "pm10Dyn": d.PM10Diff,
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
		"pm25G": mcfg.PM25L1, "pm25Y": mcfg.PM25L2, "pm25Dyn": mcfg.PM25Diff,
		"pm10G": mcfg.PM10L1, "pm10Y": mcfg.PM10L2, "pm10Dyn": mcfg.PM10Diff,
	})

	b.sendWithKeyboard(chatID, text, b.settingsKeyboard(chatID))
}
