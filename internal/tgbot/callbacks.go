// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file handles incoming callback queries from inline keyboards and dispatches
// them to the appropriate menu actions.
package tgbot

import (
	"context"
	"fmt"
	"strings"

	"sort"

	"github.com/ABespalov/aqinotifier/internal/config"
	"github.com/ABespalov/aqinotifier/internal/sensor"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"
)

// AlertItem describes a single configured notification rule for rendering in the UI.
type AlertItem struct {
	id         string
	pm         string
	action     string
	actionIcon string
	zone       string
	zoneIcon   string
	delta      string
	aqiPrefix  string
	aqiName    string
	loud       bool
}

func (ctx *RequestContext) getAllAlerts(filter string) []AlertItem {
	d := ctx.Bot.defaults
	defWarnings := make(map[string]bool)
	for _, w := range config.FlattenNotifications(d.Warnings) {
		defWarnings[w] = true
	}

	var res []AlertItem

	if filter == "" || filter == "aqi" {
		mcfg := ctx.Settings
		stdTag := strings.ToUpper(mcfg.AQI.Standard)
		stdData := sensor.GetStandard(stdTag)
		if stdData != nil {
			for _, zone := range stdData.Zones {
				id := fmt.Sprintf("aqi_l%d", zone.Level)

				name := zone.Name
				nameKey := fmt.Sprintf("aqiNameL%d%s", zone.Level, strings.Title(strings.ToLower(stdTag)))
				if loc := ctx.T(nameKey); !strings.HasPrefix(loc, "!!") {
					name = loc
				}

				res = append(res, AlertItem{
					id:        id,
					aqiPrefix: ctx.T("txtChartSubjectAqi"),
					aqiName:   name,
					zoneIcon:  zone.Icon,
					loud:      defWarnings[id],
				})
			}
		}
	}

	if filter == "" || filter == "val" || filter == "pm" {
		pmTypes := []string{"25", "10", "s"}
		actions := []string{"u", "d"}
		levels := []string{"1", "2", "3"}

		for _, pm := range pmTypes {
			for _, act := range actions {
				for _, level := range levels {
					if act == "u" && level == "1" {
						continue
					}
					if act == "d" && level == "3" {
						continue
					}

					id := fmt.Sprintf("val%s_l%s%s", pm, level, act)

					var pmLabel string
					switch pm {
					case "10":
						pmLabel = ctx.T("labelField_pm10")
					case "25":
						pmLabel = ctx.T("labelField_pm25")
					default:
						pmLabel = ctx.T("alertVsShort")
					}

					actIcon := ctx.T(kIcoTrendUp)
					if act == "d" {
						actIcon = ctx.T(kIcoTrendDown)
					}

					var zoneIcon string
					switch level {
					case "1":
						zoneIcon = ctx.T(kIcoPmLevel1)
					case "2":
						zoneIcon = ctx.T(kIcoPmLevel2)
					default:
						zoneIcon = ctx.T(kIcoPmLevel3)
					}

					actName := ctx.T("alertActionRise")
					if act == "d" {
						actName = ctx.T("alertActionFall")
					}

					var zoneName string
					switch level {
					case "1":
						zoneName = ctx.T("labelL1Acc")
					case "2":
						zoneName = ctx.T("labelL2Acc")
					default:
						zoneName = ctx.T("labelL3Acc")
					}

					res = append(res, AlertItem{
						id:         id,
						pm:         pmLabel,
						action:     actName,
						actionIcon: actIcon,
						zone:       zoneName,
						zoneIcon:   zoneIcon,
						loud:       defWarnings[id],
					})
				}
			}
		}
	}

	if filter == "" || filter == "diff" || filter == "pm" {
		pmTypes := []string{"25", "10", "s"}
		actions := []string{"u", "d"}
		levels := []string{"1", "2", "3"}
		for _, pm := range pmTypes {
			for _, act := range actions {
				for _, level := range levels {
					id := fmt.Sprintf("diff%s_l%s%s", pm, level, act)

					var pmLabel string
					switch pm {
					case "10":
						pmLabel = ctx.T("labelField_pm10")
					case "25":
						pmLabel = ctx.T("labelField_pm25")
					default:
						pmLabel = ctx.T("alertVsShort")
					}

					actIcon := ctx.T(kIcoTrendUp)
					if act == "d" {
						actIcon = ctx.T(kIcoTrendDown)
					}

					var zoneIcon string
					switch level {
					case "1":
						zoneIcon = ctx.T(kIcoPmLevel1)
					case "2":
						zoneIcon = ctx.T(kIcoPmLevel2)
					default:
						zoneIcon = ctx.T(kIcoPmLevel3)
					}

					actName := ctx.T("alertActionRise")
					if act == "d" {
						actName = ctx.T("alertActionFall")
					}

					var zoneName string
					switch level {
					case "1":
						zoneName = ctx.T("labelL1Acc")
					case "2":
						zoneName = ctx.T("labelL2Acc")
					default:
						zoneName = ctx.T("labelL3Acc")
					}

					res = append(res, AlertItem{
						id:         id,
						pm:         pmLabel,
						action:     actName,
						actionIcon: actIcon,
						zone:       zoneName,
						zoneIcon:   zoneIcon,
						delta:      ctx.T("txtLabelDelta"),
						loud:       defWarnings[id],
					})
				}
			}
		}
	}

	return res
}

func (ctx *RequestContext) handleCallback(cq *telego.CallbackQuery) {
	_ = ctx.Bot.api.AnswerCallbackQuery(context.Background(), tu.CallbackQuery(cq.ID))

	data := cq.Data
	if cq.Message == nil {
		return
	}
	chatID := cq.Message.GetChat().ID
	log.Debug().Int64("chat_id", chatID).Str("data", data).Msg("tgbot: received callback")

	switch {
	case data == "none":
		return
	case data == cmdResetSettings:
		ctx.cleanupMessage(cq)
		ctx.cmdResetConfirm()
		return

	case data == cmdHelp:
		ctx.cleanupMessage(cq)
		ctx.sendHelp()
	case data == cmdSettings:
		ctx.cleanupMessage(cq)
		ctx.cmdSettings()
	case data == cmdStatus:
		ctx.cleanupMessage(cq)
		ctx.cmdStatusMenu()
	case data == cmdCharts:
		ctx.cleanupMessage(cq)
		ctx.cmdChartsMenu()
	case data == cmdHistory:
		ctx.cleanupMessage(cq)
		ctx.cmdHistoryMenu()
	case data == cmdList:
		ctx.cleanupMessage(cq)
		ctx.cmdList()
	case data == cmdThresholdsMenu:
		ctx.cleanupMessage(cq)
		ctx.cmdThresholdsMenu()
	case data == cmdAqiCycleMenu:
		ctx.cmdAQICycleMenu(cq.Message.GetMessageID())
	case data == cmdResetDefaultsYes:
		ctx.cleanupMessage(cq)
		ctx.cmdResetExecute()
	case data == cmdSoundProfiles:
		ctx.cleanupMessage(cq)
		ctx.cmdSoundMenu(false)
	case data == cmdSilentProfiles:
		ctx.cleanupMessage(cq)
		ctx.cmdSoundMenu(true)
	case data == cmdSubscribe:
		ctx.cleanupMessage(cq)
		ctx.promptDeviceID()
	case data == cmdUnsubscribe:
		ctx.cleanupMessage(cq)
		ctx.cmdUnsubscribeMenu()
	case data == cmdAQISettings:
		ctx.cmdAqiMenu(cq.Message.GetMessageID())
	case data == cmdLang:
		ctx.cleanupMessage(cq)
		ctx.cmdLangMenu()
	case data == cmdCancelThreshold:
		ctx.deleteMessage(cq)
		ctx.Bot.setState(chatID, stateIdle)
		ctx.cmdThresholdsMenu()
	case data == cmdCancelSub:
		ctx.deleteMessage(cq)
		ctx.Bot.setState(chatID, stateIdle)
		ctx.sendHelp()
	case data == "menu_lazy":
		ctx.cleanupMessage(cq)
		ctx.cmdLazyMenu()
	case strings.HasPrefix(data, "lazy_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			ctx.promptLazy(parts[1], parts[2])
		}
	case data == "cancel_lazy":
		ctx.deleteMessage(cq)
		ctx.Bot.setState(chatID, stateIdle)
		ctx.cmdLazyMenu()
	case data == "aqi_std_toggle":
		mcfg := ctx.Settings
		allStds := sensor.GetStandards()
		tags := make([]string, 0, len(allStds))
		for tag := range allStds {
			tags = append(tags, tag)
		}
		sort.Strings(tags)

		next := tags[0]
		for i, tag := range tags {
			if tag == mcfg.AQI.Standard {
				if i+1 < len(tags) {
					next = tags[i+1]
				}
				break
			}
		}
		mcfg.AQI.Standard = next
		ctx.Bot.store.UpdateSettings(chatID, mcfg)
		ctx.cmdAqiMenu(cq.Message.GetMessageID())
	case strings.HasPrefix(data, "aqi_toggle:"):
		id := strings.TrimPrefix(data, "aqi_toggle:")
		mcfg := ctx.Settings
		mcfg.ToggleNotification(id)
		ctx.Bot.store.UpdateSettings(chatID, mcfg)
		ctx.cmdAqiMenu(cq.Message.GetMessageID())
	case strings.HasPrefix(data, "aqi_sound:"):
		id := strings.TrimPrefix(data, "aqi_sound:")
		mcfg := ctx.Settings
		mcfg.ToggleWarning(id)
		ctx.Bot.store.UpdateSettings(chatID, mcfg)
		ctx.cmdAqiMenu(cq.Message.GetMessageID())
	case strings.HasPrefix(data, "charts_dev:"):
		ctx.cleanupMessage(cq)
		deviceID := strings.TrimPrefix(data, "charts_dev:")
		ctx.sendWithKeyboard(ctx.T(msgChartsMenu), ctx.chartsMenuKeyboard(deviceID))

	case strings.HasPrefix(data, "pm_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			ctx.promptThreshold(parts[1], parts[2])
		}
	case strings.HasPrefix(data, "unsub:"):
		deviceID := strings.TrimPrefix(data, "unsub:")
		text := ctx.TDevice(msgUnsubConfirm, deviceID)
		kb := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(ctx.T(btnYes, ctx.Bot.TLang("en", kIcoSuccess))).WithCallbackData(fmt.Sprintf("unsub_yes:%s", deviceID)),
				tu.InlineKeyboardButton(ctx.T(btnNo, ctx.Bot.TLang("en", kIcoError))).WithCallbackData(fmt.Sprintf("unsub_no:%s", deviceID)),
			),
		)
		ctx.sendWithKeyboard(text, kb)

	case strings.HasPrefix(data, "unsub_yes:"):
		deviceID := strings.TrimPrefix(data, "unsub_yes:")
		ctx.Bot.store.Unsubscribe(chatID, deviceID)
		_ = ctx.Bot.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		ctx.sendWithKeyboard(ctx.TDevice(msgUnsubscribed, deviceID), nil)
		ctx.cmdList()

	case strings.HasPrefix(data, "unsub_no:"):
		ctx.cleanupMessage(cq)

	case strings.HasPrefix(data, "aqi_cycle:"):
		ctx.handleAQIThresholdCycle(data, cq.Message.GetMessageID())
		return

	case strings.HasPrefix(data, "lang_set:"):
		newLang := strings.TrimPrefix(data, "lang_set:")
		current := ctx.Bot.store.GetLanguage(chatID)
		if newLang == current {
			return
		}
		ctx.Bot.store.SetLanguage(chatID, newLang)
		ctx.Language = newLang
		ctx.cmdLangMenu(cq.Message.GetMessageID())
		ctx.Bot.updateCommandsForUser(chatID, newLang)
		return

	case data == "unit_set:temp:toggle":
		curr := ctx.Bot.store.GetUnitTemp(chatID)
		next := "c"
		if curr == "c" {
			next = "f"
		}
		ctx.Bot.store.SetUnitTemp(chatID, next)
		ctx.cmdLangMenu(cq.Message.GetMessageID())

	case data == "unit_set:press:toggle":
		curr := ctx.Bot.store.GetUnitPress(chatID)
		next := "mmhg"
		if curr == "mmhg" {
			next = "hpa"
		}
		ctx.Bot.store.SetUnitPress(chatID, next)
		ctx.cmdLangMenu(cq.Message.GetMessageID())

	case strings.HasPrefix(data, "unit_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			cat := parts[1]
			val := parts[2]
			if cat == "temp" {
				ctx.Bot.store.SetUnitTemp(chatID, val)
			} else {
				ctx.Bot.store.SetUnitPress(chatID, val)
			}
			ctx.cmdLangMenu(cq.Message.GetMessageID())
		}

	case strings.HasPrefix(data, "lang_set:"):
		lang := strings.TrimPrefix(data, "lang_set:")
		ctx.Bot.store.SetLanguage(chatID, lang)
		log.Debug().Int64("chat_id", chatID).Str("to", lang).Msg("tgbot: language changed via menu")
		ctx.Bot.updateCommandsForUser(chatID, lang)
		ctx.cmdLangMenu(cq.Message.GetMessageID())

	case strings.HasPrefix(data, "toggle:"):
		parts := strings.Split(data, ":")
		if len(parts) < 3 {
			return
		}
		alertID := parts[1]
		silent := parts[2] == "true"
		mcfg := ctx.Settings
		mcfg.ToggleNotification(alertID)
		ctx.Bot.store.UpdateSettings(chatID, mcfg)
		ctx.cmdSoundMenu(silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "toggle_sound:"):
		parts := strings.Split(data, ":")
		if len(parts) < 3 {
			return
		}
		alertID := parts[1]
		silent := parts[2] == "true"
		mcfg := ctx.Settings
		mcfg.ToggleWarning(alertID)
		ctx.Bot.store.UpdateSettings(chatID, mcfg)
		ctx.cmdSoundMenu(silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "rename:"):
		deviceID := strings.TrimPrefix(data, "rename:")
		ctx.cmdRename(deviceID)
		return
	case strings.HasPrefix(data, "rename_cancel:"):
		ctx.Bot.setState(chatID, stateIdle)
		ctx.Bot.renameIDMu.Lock()
		delete(ctx.Bot.renameIDs, chatID)
		ctx.Bot.renameIDMu.Unlock()
		ctx.deleteMessage(cq)
		ctx.cmdList()
		return
	case data == "rename_cancel":
		ctx.Bot.setState(chatID, stateIdle)
		ctx.Bot.renameIDMu.Lock()
		delete(ctx.Bot.renameIDs, chatID)
		ctx.Bot.renameIDMu.Unlock()
		ctx.deleteMessage(cq)
		ctx.cmdList()
		return

	case strings.HasPrefix(data, "status:"):
		ctx.cleanupMessage(cq)
		deviceID := strings.TrimPrefix(data, "status:")
		ctx.sendWithKeyboard(ctx.formatDeviceStatus(deviceID), ctx.mainKeyboard(deviceID))

	case strings.HasPrefix(data, "dev_settings:"):
		ctx.cleanupMessage(cq)
		deviceID := strings.TrimPrefix(data, "dev_settings:")
		text := ctx.buildDeviceSettingsText(deviceID)
		ctx.sendWithKeyboard(text, ctx.deviceSettingsKeyboard(deviceID))

	case strings.HasPrefix(data, "info:"):
		ctx.cleanupMessage(cq)
		deviceID := strings.TrimPrefix(data, "info:")
		ctx.sendWithKeyboard(ctx.formatDeviceStatus(deviceID), ctx.deviceInfoKeyboard(deviceID))

	case strings.HasPrefix(data, "chart:"):
		ctx.cleanupMessage(cq)
		parts := strings.SplitN(strings.TrimPrefix(data, "chart:"), ":", 2)
		if len(parts) == 2 {
			chartType := parts[0]
			deviceID := parts[1]
			ctx.sendChartForDevice(deviceID, chartType)
		}

	case strings.HasPrefix(data, "history:"):
		ctx.cleanupMessage(cq)
		deviceID := strings.TrimPrefix(data, "history:")
		ctx.cmdDeviceHistory(deviceID)
	}
}
