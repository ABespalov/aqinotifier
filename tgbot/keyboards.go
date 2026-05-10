package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

func (b *Bot) mainKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnStatus)).WithCallbackData(cmdStatus),
			tu.InlineKeyboardButton(b.T(chatID, btnSettings)).WithCallbackData(cmdSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnHistory)).WithCallbackData(cmdHistory),
			tu.InlineKeyboardButton(b.T(chatID, btnCharts)).WithCallbackData(cmdCharts),
		),
	)
}
func (b *Bot) settingsKeyboard(chatID int64) telego.ReplyMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSoundProfiles)).WithCallbackData(cmdSoundProfiles),
			tu.InlineKeyboardButton(b.T(chatID, btnSilentProfiles)).WithCallbackData(cmdSilentProfiles),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnAQISettings)).WithCallbackData(cmdAQISettings),
			tu.InlineKeyboardButton(b.T(chatID, btnThresholds)).WithCallbackData(cmdThresholdsMenu),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnList)).WithCallbackData(cmdList),
			tu.InlineKeyboardButton(b.T(chatID, btnResetDefaults)).WithCallbackData(cmdResetSettings),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData(cmdHelp),
		),
	)
}
func (b *Bot) resetDefaultsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnYes)).WithCallbackData("reset_defaults_yes"),
			tu.InlineKeyboardButton(b.T(chatID, btnNo)).WithCallbackData(cmdSettings),
		),
	)
}
func (b *Bot) chartsMenuKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartAQI)).WithCallbackData(fmt.Sprintf("chart:aqi:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartPM)).WithCallbackData(fmt.Sprintf("chart:pm:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartTemp)).WithCallbackData(fmt.Sprintf("chart:temp:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnChartHum)).WithCallbackData(fmt.Sprintf("chart:hum:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnChartPress)).WithCallbackData(fmt.Sprintf("chart:press:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnMainMenu)).WithCallbackData(cmdHelp),
		),
	)
}
func (b *Bot) thresholdsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Green)).WithCallbackData(cmdPM25Green),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Green)).WithCallbackData(cmdPM10Green),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Yellow)).WithCallbackData(cmdPM25Yellow),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Yellow)).WithCallbackData(cmdPM10Yellow),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnPM25Diff)).WithCallbackData(cmdPM25Diff),
			tu.InlineKeyboardButton(b.T(chatID, btnPM10Diff)).WithCallbackData(cmdPM10Diff),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSetByAQI)).WithCallbackData("menu_aqi_cycle"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSettings)).WithCallbackData(cmdSettings),
		),
	)
}
func (b *Bot) subscriptionKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnSubscribe)).WithCallbackData("menu_subscribe"),
			tu.InlineKeyboardButton(b.T(chatID, btnUnsubscribe)).WithCallbackData("menu_unsubscribe"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnBack)).WithCallbackData("menu_main"),
		),
	)
}
func (b *Bot) aqiSettingsKeyboard(chatID int64) *telego.InlineKeyboardMarkup {
	mcfg := b.GetUserSettings(chatID)
	std := mcfg.AQIStandard
	stdLabel := b.T(chatID, "standard_"+strings.ToLower(std))

	aqiAlerts := []string{"aqi_z1", "aqi_z2", "aqi_z3", "aqi_z4", "aqi_z5", "aqi_z6"}
	if std == "US" {
		aqiAlerts = append(aqiAlerts, "aqi_z7")
	}
	activeNotifications := make(map[string]bool)
	for _, n := range mcfg.Notifications {
		activeNotifications[n] = true
	}
	loudWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		loudWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, "btnAqiStandard", map[string]interface{}{
			"std": stdLabel, "aqi_standard_flag": "@flag_" + strings.ToLower(std) + "@",
		})).WithCallbackData("aqi_std_toggle"),
	})

	stdLower := strings.ToLower(std)
	for _, id := range aqiAlerts {
		statusIcon := IconUnchecked
		if activeNotifications[id] {
			statusIcon = IconChecked
		}
		soundIcon := IconSilent
		soundLabel := "btnWithoutSound"
		if loudWarnings[id] {
			soundIcon = IconLoud
			soundLabel = "btnWithSound"
		}

		levelChar := strings.TrimPrefix(id, "aqi_")
		name := b.T(chatID, "aqi_name_"+levelChar+"_"+stdLower)

		var zoneIcon string
		if std == "US" {
			icons := map[string]string{"z1": IconGreen, "z2": IconYellow, "z3": IconOrange, "z4": IconRed, "z5": IconPurple, "z6": IconMaroon, "z7": IconBlack}
			zoneIcon = icons[levelChar]
		} else {
			icons := map[string]string{"z1": IconBlue, "z2": IconGreen, "z3": IconYellow, "z4": IconOrange, "z5": IconRed, "z6": IconMaroon}
			zoneIcon = icons[levelChar]
		}

		btnText := b.T(chatID, "alertAqiBtn", map[string]interface {
		}{"icon": name, "label": zoneIcon})

		soundLabelStr := b.T(chatID, soundLabel)
		callbackData := fmt.Sprintf("aqi_sound:%s", id)
		if !activeNotifications[id] {
			soundLabelStr = b.T(chatID, "btnInactive")
			soundIcon = ""
			callbackData = "none"
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, btnText)).
				WithCallbackData(fmt.Sprintf("aqi_toggle:%s", id)),
			tu.InlineKeyboardButton(strings.TrimSpace(fmt.Sprintf("%s %s", soundIcon, soundLabelStr))).
				WithCallbackData(callbackData),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, btnMonSettings)).WithCallbackData("menu_settings"),
	})

	return tu.InlineKeyboard(rows...)
}
func (b *Bot) notificationSettingsKeyboard(chatID int64, silent bool) *telego.InlineKeyboardMarkup {
	mcfg := b.GetUserSettings(chatID)
	allAlerts := b.getAllAlerts(chatID)

	activeNotifications := make(map[string]bool)
	for _, n := range mcfg.Notifications {
		activeNotifications[n] = true
	}

	activeWarnings := make(map[string]bool)
	for _, w := range mcfg.Warnings {
		activeWarnings[w] = true
	}

	var rows [][]telego.InlineKeyboardButton
	for _, a := range allAlerts {
		if a.loud != !silent {
			continue
		}

		statusIcon := IconUnchecked
		if activeNotifications[a.id] {
			statusIcon = IconChecked
		}

		soundIcon := IconSilent
		soundLabel := "btnWithoutSound"
		if activeWarnings[a.id] {
			soundIcon = IconLoud
			soundLabel = "btnWithSound"
		}

		soundLabelStr := b.T(chatID, soundLabel)
		callbackData := fmt.Sprintf("toggle_sound:%s:%t", a.id, silent)
		if !activeNotifications[a.id] {
			soundLabelStr = b.T(chatID, "btnInactive")
			soundIcon = ""
			callbackData = "none"
		}

		rows = append(rows, []telego.InlineKeyboardButton{
			tu.InlineKeyboardButton(fmt.Sprintf("%s %s", statusIcon, a.name)).
				WithCallbackData(fmt.Sprintf("toggle:%s:%t", a.id, silent)),
			tu.InlineKeyboardButton(strings.TrimSpace(fmt.Sprintf("%s %s", soundIcon, soundLabelStr))).
				WithCallbackData(callbackData),
		})
	}

	rows = append(rows, []telego.InlineKeyboardButton{
		tu.InlineKeyboardButton(b.T(chatID, "btnMonSettings")).WithCallbackData("menu_settings"),
	})

	return tu.InlineKeyboard(rows...)
}
func (b *Bot) deviceInfoKeyboard(chatID int64, deviceID string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, "btnRename")).WithCallbackData(fmt.Sprintf("rename:%s", deviceID)),
			tu.InlineKeyboardButton(b.T(chatID, btnUnsubscribe)).WithCallbackData(fmt.Sprintf("unsub:%s", deviceID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(b.T(chatID, btnList)).WithCallbackData(cmdList),
		),
	)
}
func (b *Bot) sendWithKeyboard(chatID int64, text string, markup telego.ReplyMarkup) {
	params := tu.Message(tu.ID(chatID), text).
		WithReplyMarkup(markup).
		WithParseMode(telego.ModeHTML)

	_, _ = b.api.SendMessage(context.Background(), params)
}
