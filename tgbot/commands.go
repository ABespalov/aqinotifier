package tgbot

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"

	"github.com/ABespalov/aqinotifier/sensor"
)

func (b *Bot) cmdLangMenu(chatID int64) {
	currentLang := b.store.GetLanguage(chatID)
	currentTemp := b.store.GetUnitTemp(chatID)
	currentPress := b.store.GetUnitPress(chatID)

	langs := AvailableLanguages()
	var langBtns []telego.InlineKeyboardButton
	for _, lang := range langs {
		label := b.TLang(lang, "lang_"+lang)
		if lang == currentLang || (currentLang == "" && lang == "en") {
			label = IconSuccess + " " + label
		}
		langBtns = append(langBtns, tu.InlineKeyboardButton(label).WithCallbackData("lang_set:"+lang))
	}

	btnC := b.T(chatID, "unit_c")
	btnF := b.T(chatID, "unit_f")
	if currentTemp == "c" {
		btnC = IconSuccess + " " + btnC
	} else {
		btnF = IconSuccess + " " + btnF
	}

	btnMMHG := b.T(chatID, "unit_mmhg")
	btnHPA := b.T(chatID, "unit_hpa")
	if currentPress == "mmhg" {
		btnMMHG = IconSuccess + " " + btnMMHG
	} else {
		btnHPA = IconSuccess + " " + btnHPA
	}

	inlineKeyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(langBtns...),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnC).WithCallbackData("unit_set:temp:c"),
			tu.InlineKeyboardButton(btnF).WithCallbackData("unit_set:temp:f"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnMMHG).WithCallbackData("unit_set:press:mmhg"),
			tu.InlineKeyboardButton(btnHPA).WithCallbackData("unit_set:press:hpa"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData("menu_main"),
		),
	)

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_lang")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(inlineKeyboard)
	_, _ = b.api.SendMessage(context.Background(), params)
}
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
			b.sendWithKeyboard(chatID, b.T(chatID, "msg_invalid_device_id"), b.mainKeyboard(chatID))
			return
		}
	}

	var text string
	if b.store.Subscribe(chatID, deviceID, b.defaults) {
		text = b.T(chatID, "msg_subscribed", map[string]interface {
		}{"device_id": deviceID})
	} else {
		text = b.T(chatID, "msg_already_sub", map[string]interface {
		}{"device_id": deviceID})
	}
	b.sendWithKeyboard(chatID, text, nil)
	b.cmdList(chatID)
}
func (b *Bot) cmdList(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		text := b.T(chatID, "msg_no_subs")
		b.sendWithKeyboard(chatID, text, b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		mcfg := b.GetUserSettings(chatID)
		label := id
		if name, ok := mcfg.DeviceNames[id]; ok && name != "" {
			label = fmt.Sprintf("%s (%s)", name, id)
		}
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconStatus, label)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnSubscribe)).WithCallbackData("menu_subscribe"),
		tu.InlineKeyboardButton(b.T(chatID, btnBack)).WithCallbackData("menu_main"),
	})

	text := b.T(chatID, "msg_your_subs") + "\n\n<b>" + b.T(chatID, "msg_manage_subs") + "</b>"
	params := tu.Message(tu.ID(chatID), text).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdUnsubscribeMenu(chatID int64) {
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs"), b.subscriptionKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s  %s", IconUnsubscribe, id)).WithCallbackData(fmt.Sprintf("unsub:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnBack)).WithCallbackData("menu_main"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_unsub",
		IconDelete)).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdStatusMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs"), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, devices[0]), b.mainKeyboard(chatID))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconStatus, id)).WithCallbackData(fmt.Sprintf("status:%s", id)),
		})
	}

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdSettings(chatID int64) {
	ReloadAll()
	b.clearLastPrompt(chatID)
	b.sendWithKeyboard(chatID, b.T(chatID, "msg_settings_title"), b.settingsKeyboard(chatID))
}
func (b *Bot) cmdChartsMenu(chatID int64) {
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs"), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_charts_menu"), b.chartsMenuKeyboard(chatID, devices[0]))
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconStatus, id)).WithCallbackData(fmt.Sprintf("charts_dev:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData("menu_main"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_device")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdAqiMenu(chatID int64, editMsgID ...int) {
	text := b.T(chatID, "msg_aqi_settings")
	kb := b.aqiSettingsKeyboard(chatID)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		params := tu.Message(tu.ID(chatID), text).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.SendMessage(context.Background(), params)
	}
}
func (b *Bot) cmdThresholdsMenu(chatID int64) {
	mcfg := b.GetUserSettings(chatID)
	text := b.T(chatID, "msg_thresholds_menu", map[string]interface{}{
		"vg1": mcfg.PM25Green, "vy1": mcfg.PM25Yellow, "vdyn1": mcfg.PM25Diff,
		"vg2": mcfg.PM10Green, "vy2": mcfg.PM10Yellow, "vdyn2": mcfg.PM10Diff,
	})

	b.sendWithKeyboard(chatID, text, b.thresholdsKeyboard(chatID))
}
func (b *Bot) cmdAQICycleMenu(chatID int64, editMsgID ...int) {
	mcfg := b.GetUserSettings(chatID)

	text := b.T(chatID, "msg_aqi_cycle_menu", map[string]interface{}{
		"vg1": mcfg.PM25Green, "vy1": mcfg.PM25Yellow,
		"vg2": mcfg.PM10Green, "vy2": mcfg.PM10Yellow,
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
					flag = b.T(chatID, "flag_eu")
					found = true
					break
				}
			}
			if !found {
				for _, v := range us {
					if v == val {
						std = "US"
						flag = b.T(chatID, "flag_us")
						found = true
						break
					}
				}
			}
		} else {
			for _, v := range us {
				if v == val {
					std = "US"
					flag = b.T(chatID, "flag_us")
					found = true
					break
				}
			}
			if !found {
				for _, v := range eu {
					if v == val {
						std = "EU"
						flag = b.T(chatID, "flag_eu")
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
		return IconWrite, ""
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
			tu.InlineKeyboardButton(btnText("PM2.5", IconGreenSq, mcfg.PM25Green)).WithCallbackData("aqi_cycle:PM2.5:green"),
			tu.InlineKeyboardButton(btnText("PM10", IconGreenSq, mcfg.PM10Green)).WithCallbackData("aqi_cycle:PM10:green"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnText("PM2.5", IconYellowSq, mcfg.PM25Yellow)).WithCallbackData("aqi_cycle:PM2.5:yellow"),
			tu.InlineKeyboardButton(btnText("PM10", IconYellowSq, mcfg.PM10Yellow)).WithCallbackData("aqi_cycle:PM10:yellow"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnThresholds)).WithCallbackData(cmdThresholdsMenu),
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
	b.clearLastPrompt(chatID)
	devices := b.store.Subscriptions(chatID)
	if len(devices) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_no_subs"), b.subscriptionKeyboard(chatID))
		return
	}
	if len(devices) == 1 {
		b.cmdDeviceHistory(chatID, devices[0])
		return
	}

	var rows [][]telego.InlineKeyboardButton
	for _, id := range devices {
		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", IconHistory, id)).WithCallbackData(fmt.Sprintf("history:%s", id)),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData("menu_main"),
	})

	params := tu.Message(tu.ID(chatID), b.T(chatID, "msg_select_history")).
		WithParseMode(telego.ModeHTML).
		WithReplyMarkup(tu.InlineKeyboard(rows...))
	_, _ = b.api.SendMessage(context.Background(), params)
}
func (b *Bot) cmdSoundMenu(chatID int64, silent bool, editMsgID ...int) {
	var sb strings.Builder
	if silent {
		sb.WriteString(b.T(chatID, "msg_silent_alerts"))
	} else {
		sb.WriteString(b.T(chatID, "msg_loud_alerts"))
	}
	sb.WriteString(b.T(chatID, "msg_sound_settings"))

	kb := b.notificationSettingsKeyboard(chatID, silent)

	if len(editMsgID) > 0 {
		params := tu.EditMessageText(tu.ID(chatID), editMsgID[0], sb.String()).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.EditMessageText(context.Background(), params)
	} else {
		params := tu.Message(tu.ID(chatID), sb.String()).
			WithParseMode(telego.ModeHTML).
			WithReplyMarkup(kb)
		_, _ = b.api.SendMessage(context.Background(), params)
	}
}
func (b *Bot) cmdRename(chatID int64, deviceID string) {
	b.setState(chatID, stateAwaitDeviceName)
	b.renameIDMu.Lock()
	b.renameIDs[chatID] = deviceID
	b.renameIDMu.Unlock()

	text := b.T(chatID, "msg_rename_prompt", map[string]interface {
	}{"device_id": deviceID})
	keyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "msg_rename_cancel")).WithCallbackData(fmt.Sprintf("rename_cancel:%s", deviceID)),
		),
	)
	b.sendWithKeyboard(chatID, text, keyboard)
}
func (b *Bot) cmdDeviceHistory(chatID int64, deviceID string, msgID ...int) {

	if len(msgID) > 0 {
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{
			ChatID:    tu.ID(chatID),
			MessageID: msgID[0],
		})
	}

	hist := b.monitor.GetHistory(deviceID)
	if len(hist) == 0 {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_history_empty", map[string]interface {
		}{"device_id": deviceID}), b.mainKeyboard(chatID))
		return
	}

	log.Debug().Msgf("Generating charts with width=%d, height=%d, fontSize=%.1f", b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	buffers, err := generateCharts(b, chatID, hist, b.cfg.ChartWidth, b.cfg.ChartHeight, b.cfg.ChartFontSize)
	if err != nil {
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_charts", map[string]interface {
		}{"err": err}), b.mainKeyboard(chatID))
		return
	}

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
	_, err = b.api.SendMediaGroup(context.Background(), params)
	if err != nil {
		log.Error().Err(err).Msg("failed to send history charts")
		b.sendWithKeyboard(chatID, b.T(chatID, "msg_error_send_ch"), b.mainKeyboard(chatID))
		return
	}

	footer := b.T(chatID, "msg_history_footer", map[string]interface {
	}{"count": b.sys.ValuesInRam, "device_id": b.formatDeviceID(chatID, deviceID)})
	b.sendWithKeyboard(chatID, footer, b.mainKeyboard(chatID))
}
func (b *Bot) cmdResetConfirm(chatID int64) {
	d := b.defaults
	std := strings.ToLower(d.AQIStandard)
	stdLabel := b.T(chatID, "standard_"+std)



	unitT := b.T(chatID, "unit_"+b.cfg.DefaultUnitTemp)
	unitP := b.T(chatID, "unit_"+b.cfg.DefaultUnitPress)

	var alertsSB strings.Builder
	allAlerts := b.getAllAlerts(chatID)

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
			icon := "•"
			if defWarnings[a.id] {
				icon = IconLoud
			} else {
				icon = IconSilent
			}
			alertsSB.WriteString(fmt.Sprintf("%s %s\n", icon, a.name))
		}
	}

	details := b.T(chatID, "msg_reset_confirm_details", map[string]interface{}{
		"pm25_g": d.PM25Green, "pm25_y": d.PM25Yellow, "pm25_dyn": d.PM25Diff,
		"pm10_g": d.PM10Green, "pm10_y": d.PM10Yellow, "pm10_dyn": d.PM10Diff,
		"std_name": stdLabel, "aqi_standard_flag": "@flag_"+strings.ToLower(std)+"@",
		"unit_t": unitT, "unit_p": unitP,
		"alerts_list": alertsSB.String(),
	})

	text := b.T(chatID, "msg_reset_confirm") + details
	b.sendWithKeyboard(chatID, text, b.resetDefaultsKeyboard(chatID))
}
func (b *Bot) cmdResetExecute(chatID int64) {
	b.store.ResetSettings(chatID, b.defaults)

	mcfg := b.store.GetSettings(chatID, b.defaults)
	text := b.T(chatID, "msg_reset_done", map[string]interface{}{
		"pm25_g": mcfg.PM25Green, "pm25_y": mcfg.PM25Yellow, "pm25_dyn": mcfg.PM25Diff,
		"pm10_g": mcfg.PM10Green, "pm10_y": mcfg.PM10Yellow, "pm10_dyn": mcfg.PM10Diff,
	})

	b.sendWithKeyboard(chatID, text, b.settingsKeyboard(chatID))
}
