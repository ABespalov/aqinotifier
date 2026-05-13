package tgbot

import (
	"context"
	"fmt"
	"strings"

	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/rs/zerolog/log"
	"sort"
)

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

func (b *Bot) getAllAlerts(chatID int64, filter string) []AlertItem {
	d := b.defaults
	defWarnings := make(map[string]bool)
	for _, w := range d.Warnings {
		defWarnings[w] = true
	}

	var res []AlertItem

	if filter == "" || filter == "aqi" {
		mcfg := b.GetUserSettings(chatID)
		stdTag := strings.ToUpper(mcfg.AQIStandard)
		stdData, ok := sensor.Standards[stdTag]
		if ok {
			for _, zone := range stdData.Zones {
				id := fmt.Sprintf("aqi_l%d", zone.Level)
				
				name := zone.Name
				nameKey := fmt.Sprintf("aqiNameL%d%s", zone.Level, strings.Title(strings.ToLower(stdTag)))
				if loc := b.T(chatID, nameKey); !strings.HasPrefix(loc, "!!") {
					name = loc
				}

				res = append(res, AlertItem{
					id:        id,
					aqiPrefix: b.T(chatID, "txtChartSubjectAqi"),
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
						pmLabel = b.T(chatID, "alertV10Short")
					case "25":
						pmLabel = b.T(chatID, "alertV25Short")
					default:
						pmLabel = b.T(chatID, "alertVsShort")
					}

					actIcon := b.I(kIcoTrendUp)
					if act == "d" {
						actIcon = b.I(kIcoTrendDown)
					}

					var zoneIcon string
					switch level {
					case "1":
						zoneIcon = b.I(kIcoPmLevel1)
					case "2":
						zoneIcon = b.I(kIcoPmLevel2)
					default:
						zoneIcon = b.I(kIcoPmLevel3)
					}

					actName := b.T(chatID, "alertActionRise")
					if act == "d" {
						actName = b.T(chatID, "alertActionFall")
					}

					var zoneName string
					switch level {
					case "1":
						zoneName = b.T(chatID, "labelL1Acc")
					case "2":
						zoneName = b.T(chatID, "labelL2Acc")
					default:
						zoneName = b.T(chatID, "labelL3Acc")
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
						pmLabel = b.T(chatID, "alertV10Short")
					case "25":
						pmLabel = b.T(chatID, "alertV25Short")
					default:
						pmLabel = b.T(chatID, "alertVsShort")
					}

					actIcon := b.I(kIcoTrendUp)
					if act == "d" {
						actIcon = b.I(kIcoTrendDown)
					}

					var zoneIcon string
					switch level {
					case "1":
						zoneIcon = b.I(kIcoPmLevel1)
					case "2":
						zoneIcon = b.I(kIcoPmLevel2)
					default:
						zoneIcon = b.I(kIcoPmLevel3)
					}

					actName := b.T(chatID, "alertActionRise")
					if act == "d" {
						actName = b.T(chatID, "alertActionFall")
					}

					var zoneName string
					switch level {
					case "1":
						zoneName = b.T(chatID, "labelL1Acc")
					case "2":
						zoneName = b.T(chatID, "labelL2Acc")
					default:
						zoneName = b.T(chatID, "labelL3Acc")
					}

					res = append(res, AlertItem{
						id:         id,
						pm:         pmLabel,
						action:     actName,
						actionIcon: actIcon,
						zone:       zoneName,
						zoneIcon:   zoneIcon,
						delta:      b.T(chatID, "txtLabelDelta"),
						loud:       defWarnings[id],
					})
				}
			}
		}
	}

	return res
}

func (b *Bot) handleCallback(cq *telego.CallbackQuery) {
	_ = b.api.AnswerCallbackQuery(context.Background(), tu.CallbackQuery(cq.ID))

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
		b.cleanupMessage(chatID, cq)
		b.cmdResetConfirm(chatID)
		return

	case data == cmdHelp:
		b.cleanupMessage(chatID, cq)
		b.sendHelp(chatID)
	case data == cmdSettings:
		b.cleanupMessage(chatID, cq)
		b.cmdSettings(chatID)
	case data == cmdStatus:
		b.cleanupMessage(chatID, cq)
		b.cmdStatusMenu(chatID)
	case data == cmdCharts:
		b.cleanupMessage(chatID, cq)
		b.cmdChartsMenu(chatID)
	case data == cmdHistory:
		b.cleanupMessage(chatID, cq)
		b.cmdHistoryMenu(chatID)
	case data == cmdList:
		b.cleanupMessage(chatID, cq)
		b.cmdList(chatID)
	case data == cmdThresholdsMenu:
		b.cleanupMessage(chatID, cq)
		b.cmdThresholdsMenu(chatID)
	case data == cmdAqiCycleMenu:
		b.cmdAQICycleMenu(chatID, cq.Message.GetMessageID())
	case data == cmdResetDefaultsYes:
		b.cleanupMessage(chatID, cq)
		b.cmdResetExecute(chatID)
	case data == cmdSoundProfiles:
		b.cleanupMessage(chatID, cq)
		b.cmdSoundMenu(chatID, false)
	case data == cmdSilentProfiles:
		b.cleanupMessage(chatID, cq)
		b.cmdSoundMenu(chatID, true)
	case data == cmdSubscribe:
		b.cleanupMessage(chatID, cq)
		b.promptDeviceID(chatID)
	case data == cmdUnsubscribe:
		b.cleanupMessage(chatID, cq)
		b.cmdUnsubscribeMenu(chatID)
	case data == cmdAQISettings:
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case data == cmdLang:
		b.cleanupMessage(chatID, cq)
		b.cmdLangMenu(chatID)
	case data == cmdCancelThreshold:
		b.cleanupMessage(chatID, cq)
		b.setState(chatID, stateIdle)
	case data == cmdCancelSub:
		b.cleanupMessage(chatID, cq)
		b.setState(chatID, stateIdle)
	case data == "aqi_std_toggle":
		mcfg := b.GetUserSettings(chatID)
		tags := make([]string, 0, len(sensor.Standards))
		for tag := range sensor.Standards {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		
		next := tags[0]
		for i, tag := range tags {
			if tag == mcfg.AQIStandard {
				if i+1 < len(tags) {
					next = tags[i+1]
				}
				break
			}
		}
		mcfg.AQIStandard = next
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case strings.HasPrefix(data, "aqi_toggle:"):
		id := strings.TrimPrefix(data, "aqi_toggle:")
		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, n := range mcfg.Notifications {
			if n == id {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Notifications = append(mcfg.Notifications[:found], mcfg.Notifications[found+1:]...)
		} else {
			mcfg.Notifications = append(mcfg.Notifications, id)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case strings.HasPrefix(data, "aqi_sound:"):
		id := strings.TrimPrefix(data, "aqi_sound:")
		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, w := range mcfg.Warnings {
			if w == id {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Warnings = append(mcfg.Warnings[:found], mcfg.Warnings[found+1:]...)
		} else {
			mcfg.Warnings = append(mcfg.Warnings, id)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdAqiMenu(chatID, cq.Message.GetMessageID())
	case strings.HasPrefix(data, "charts_dev:"):
		b.cleanupMessage(chatID, cq)
		deviceID := strings.TrimPrefix(data, "charts_dev:")
		b.sendWithKeyboard(chatID, b.T(chatID, msgChartsMenu), b.chartsMenuKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "pm_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			b.promptThreshold(chatID, parts[1], parts[2])
		}
	case strings.HasPrefix(data, "unsub:"):
		deviceID := strings.TrimPrefix(data, "unsub:")
		text := b.TDevice(chatID, msgUnsubConfirm, deviceID)
		kb := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(b.T(chatID, btnYes, b.I(kIcoSuccess))).WithCallbackData(fmt.Sprintf("unsub_yes:%s", deviceID)),
				tu.InlineKeyboardButton(b.T(chatID, btnNo, b.I(kIcoError))).WithCallbackData(fmt.Sprintf("unsub_no:%s", deviceID)),
			),
		)
		b.sendWithKeyboard(chatID, text, kb)

	case strings.HasPrefix(data, "unsub_yes:"):
		deviceID := strings.TrimPrefix(data, "unsub_yes:")
		b.store.Unsubscribe(chatID, deviceID)
		_ = b.api.DeleteMessage(context.Background(), &telego.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: cq.Message.GetMessageID()})
		b.sendWithKeyboard(chatID, b.TDevice(chatID, msgUnsubscribed, deviceID), nil)
		b.cmdList(chatID)

	case strings.HasPrefix(data, "unsub_no:"):
		b.cleanupMessage(chatID, cq)

	case strings.HasPrefix(data, "aqi_cycle:"):
		b.handleAQIThresholdCycle(chatID, data, cq.Message.GetMessageID())
		return

	case data == "lang_cycle":
		langs := AvailableLanguages()
		current := b.store.GetLanguage(chatID)
		next := langs[0]
		for i, l := range langs {
			if l == current {
				if i+1 < len(langs) {
					next = langs[i+1]
				}
				break
			}
		}
		b.store.SetLanguage(chatID, next)
		b.updateCommandsForUser(chatID, next)
		b.cmdLangMenu(chatID, cq.Message.GetMessageID())

	case data == "unit_set:temp:toggle":
		curr := b.store.GetUnitTemp(chatID)
		next := "c"
		if curr == "c" {
			next = "f"
		}
		b.store.SetUnitTemp(chatID, next)
		b.cmdLangMenu(chatID, cq.Message.GetMessageID())

	case data == "unit_set:press:toggle":
		curr := b.store.GetUnitPress(chatID)
		next := "mmhg"
		if curr == "mmhg" {
			next = "hpa"
		}
		b.store.SetUnitPress(chatID, next)
		b.cmdLangMenu(chatID, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "unit_set:"):
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			cat := parts[1]
			val := parts[2]
			if cat == "temp" {
				b.store.SetUnitTemp(chatID, val)
			} else {
				b.store.SetUnitPress(chatID, val)
			}
			b.cmdLangMenu(chatID, cq.Message.GetMessageID())
		}

	case strings.HasPrefix(data, "lang_set:"):
		lang := strings.TrimPrefix(data, "lang_set:")
		b.store.SetLanguage(chatID, lang)
		log.Debug().Int64("chat_id", chatID).Str("to", lang).Msg("tgbot: language changed via menu")
		b.updateCommandsForUser(chatID, lang)
		b.cmdLangMenu(chatID, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "toggle:"):
		parts := strings.Split(data, ":")
		if len(parts) < 3 {
			return
		}
		alertID := parts[1]
		silent := parts[2] == "true"

		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, n := range mcfg.Notifications {
			if n == alertID {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Notifications = append(mcfg.Notifications[:found], mcfg.Notifications[found+1:]...)
		} else {
			mcfg.Notifications = append(mcfg.Notifications, alertID)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdSoundMenu(chatID, silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "toggle_sound:"):
		parts := strings.Split(data, ":")
		if len(parts) < 3 {
			return
		}
		alertID := parts[1]
		silent := parts[2] == "true"

		mcfg := b.GetUserSettings(chatID)
		found := -1
		for i, w := range mcfg.Warnings {
			if w == alertID {
				found = i
				break
			}
		}
		if found >= 0 {
			mcfg.Warnings = append(mcfg.Warnings[:found], mcfg.Warnings[found+1:]...)
		} else {
			mcfg.Warnings = append(mcfg.Warnings, alertID)
		}
		b.store.UpdateSettings(chatID, mcfg)
		b.cmdSoundMenu(chatID, silent, cq.Message.GetMessageID())

	case strings.HasPrefix(data, "rename:"):
		deviceID := strings.TrimPrefix(data, "rename:")
		b.cmdRename(chatID, deviceID)
		return
	case strings.HasPrefix(data, "rename_cancel:"):
		b.setState(chatID, stateIdle)
		b.renameIDMu.Lock()
		delete(b.renameIDs, chatID)
		b.renameIDMu.Unlock()
		b.cleanupMessage(chatID, cq)
		return
	case data == "rename_cancel":
		b.setState(chatID, stateIdle)
		b.renameIDMu.Lock()
		delete(b.renameIDs, chatID)
		b.renameIDMu.Unlock()
		b.cleanupMessage(chatID, cq)
		return

	case strings.HasPrefix(data, "status:"):
		b.cleanupMessage(chatID, cq)
		deviceID := strings.TrimPrefix(data, "status:")
		b.sendPersistentWithKeyboard(chatID, b.formatDeviceStatus(chatID, deviceID), b.mainKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "dev_settings:"):
		b.cleanupMessage(chatID, cq)
		deviceID := strings.TrimPrefix(data, "dev_settings:")
		text := b.TDevice(chatID, "msgDeviceSettingsTitle", deviceID) +
			"\n\n" + b.TDevice(chatID, "txtDevice", deviceID) +
			"\n\n" + b.T(chatID, "msgDeviceSettingsHint")
		b.sendWithKeyboard(chatID, text, b.deviceSettingsKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "info:"):
		b.cleanupMessage(chatID, cq)
		deviceID := strings.TrimPrefix(data, "info:")
		b.sendWithKeyboard(chatID, b.formatDeviceStatus(chatID, deviceID), b.deviceInfoKeyboard(chatID, deviceID))

	case strings.HasPrefix(data, "chart:"):
		b.cleanupMessage(chatID, cq)
		parts := strings.SplitN(strings.TrimPrefix(data, "chart:"), ":", 2)
		if len(parts) == 2 {
			chartType := parts[0]
			deviceID := parts[1]
			b.sendChartForDevice(chatID, deviceID, chartType)
		}

	case strings.HasPrefix(data, "history:"):
		b.cleanupMessage(chatID, cq)
		deviceID := strings.TrimPrefix(data, "history:")
		b.cmdDeviceHistory(chatID, deviceID)
	}
}
