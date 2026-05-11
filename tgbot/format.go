package tgbot

import (
	"fmt"
	"strings"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

func (b *Bot) getEventPriority(id string) int {
	switch {
	case strings.HasPrefix(id, "aqi_"):
		return 70
	case strings.HasPrefix(id, "vals-"):
		return 60
	case strings.HasPrefix(id, "val25-"):
		return 50
	case strings.HasPrefix(id, "val10-"):
		return 40
	case strings.HasPrefix(id, "diffs-"):
		return 30
	case strings.HasPrefix(id, "diff25-"):
		return 20
	case strings.HasPrefix(id, "diff10-"):
		return 10
	default:
		return 0
	}
}
func (b *Bot) getAQIIcon(level sensor.AQILevel, standard string) string {
	if strings.ToUpper(standard) == "US" {
		switch level {
		case sensor.LevelGood:
			return icoAqiUSLevel1
		case sensor.LevelModerate:
			return icoAqiUSLevel2
		case sensor.LevelSlightlyUnhealthy:
			return icoAqiUSLevel3
		case sensor.LevelUnhealthy:
			return icoAqiUSLevel4
		case sensor.LevelVeryUnhealthy:
			return icoAqiUSLevel5
		case sensor.LevelHazardous:
			return icoAqiUSLevel6
		case sensor.LevelExtremelyHazardous:
			return icoAqiUSLevel7
		}
	} else {
		switch level {
		case sensor.LevelGood:
			return icoAqiEULevel1
		case sensor.LevelModerate:
			return icoAqiEULevel2
		case sensor.LevelSlightlyUnhealthy:
			return icoAqiEULevel3
		case sensor.LevelUnhealthy:
			return icoAqiEULevel4
		case sensor.LevelVeryUnhealthy:
			return icoAqiEULevel5
		case sensor.LevelHazardous, sensor.LevelExtremelyHazardous:
			return icoAqiEULevel6
		}
	}
	return icoUnknown
}
func (b *Bot) formatDeviceStatus(chatID int64, deviceID string) string {
	log.Debug().Int64("chat_id", chatID).Str("device_id", deviceID).Msg("tgbot: formatDeviceStatus start")
	m := b.monitor.LastMeasurement(deviceID)
	if m == nil {
		log.Debug().Int64("chat_id", chatID).Str("device_id", deviceID).Msg("tgbot: no measurement found")
		return b.TDevice(chatID, "msgStatusNoData", deviceID)
	}
	res := b.formatMeasurement(chatID, m)
	log.Debug().Int64("chat_id", chatID).Int("len", len(res)).Msg("tgbot: formatDeviceStatus end")
	return res
}
func (b *Bot) formatDeviceShortInfo(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if !ok || name == "" {
		name = b.T(chatID, "msgDevice") + " " + deviceID
	}
	header := b.T(chatID, "msgDeviceInfoHeader")
	return fmt.Sprintf("%s\n\n<code>%s</code>\n%s", header, deviceID, name)
}
func (b *Bot) formatDeviceIDPlain(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s (%s)", name, deviceID)
	}
	return fmt.Sprintf("%s %s", b.T(chatID, "msgDevice"), deviceID)
}
func (b *Bot) convertTemp(celsius float64, chatID int64) float64 {
	unit := b.store.GetUnitTemp(chatID)
	if unit == "f" {
		return celsius*1.8 + 32
	}
	return celsius
}
func (b *Bot) convertPress(hpa float64, chatID int64) float64 {
	unit := b.store.GetUnitPress(chatID)
	if unit == "mmhg" {
		return hpa * hPaToMmHg
	}
	return hpa
}
func (b *Bot) unitTempLabel(chatID int64) string {
	unit := b.store.GetUnitTemp(chatID)
	if unit == "f" {
		return "°F"
	}
	return "°C"
}
func (b *Bot) unitPressLabel(chatID int64) string {
	unit := b.store.GetUnitPress(chatID)
	if unit == "mmhg" {
		return b.T(chatID, "msgUnitMmhg")
	}
	return b.T(chatID, "unitHpa")
}

func (b *Bot) buildMeasurementArgs(chatID int64, m *monitor.Measurement) map[string]interface{} {
	mcfg := b.GetUserSettings(chatID)
	std := strings.ToLower(mcfg.AQIStandard)

	args := map[string]interface{}{
		"date": m.Timestamp,
		"time": m.Timestamp,

		"deviceId": m.DeviceID,

		"val25":      m.PM25,
		"prev25":     m.PM25Prev,
		"curr25":     m.PM25,
		"unitPm":     b.T(chatID, "unitPm"),
		"val10":      m.PM10,
		"prev10":     m.PM10Prev,
		"curr10":     m.PM10,
		"labelPm25":  b.T(chatID, "labelPm25"),
		"labelPm10":  b.T(chatID, "labelPm10"),
	}

	if name, ok := mcfg.DeviceNames[m.DeviceID]; ok && name != "" {
		args["deviceName"] = name
	} else {
		args["deviceName"] = b.T(chatID, "msgDevice") + " " + m.DeviceID
	}

	if m.PM25Diff != nil {
		args["diff25Percent"] = *m.PM25Diff
	} else {
		args["diff25Percent"] = 0.0
	}
	if m.PM10Diff != nil {
		args["diff10Percent"] = *m.PM10Diff
	} else {
		args["diff10Percent"] = 0.0
	}

	diff25 := m.PM25 - m.PM25Prev
	if diff25 > 0 {
		args["trend25Icon"] = icoTrendUp
	} else if diff25 < 0 {
		args["trend25Icon"] = icoTrendDown
	} else {
		args["trend25Icon"] = icoTrendFlat
	}
	args["diff25"] = diff25

	diff10 := m.PM10 - m.PM10Prev
	if diff10 > 0 {
		args["trend10Icon"] = icoTrendUp
	} else if diff10 < 0 {
		args["trend10Icon"] = icoTrendDown
	} else {
		args["trend10Icon"] = icoTrendFlat
	}
	args["diff10"] = diff10

	getZoneIcon := func(v, g, y float64) string {
		if v <= g {
			return icoGreenSq
		}
		if v <= y {
			return icoYellowSq
		}
		return icoRedSq
	}
	args["zone25Icon"] = getZoneIcon(m.PM25, mcfg.PM25Green, mcfg.PM25Yellow)
	args["zone10Icon"] = getZoneIcon(m.PM10, mcfg.PM10Green, mcfg.PM10Yellow)

	var aqi float64
	var level sensor.AQILevel
	if mcfg.AQIStandard == "US" {
		aqi, level = sensor.CalculateUS_AQI(m.PM25, m.PM10)
	} else {
		aqi, level = sensor.CalculateEU_AQI(m.PM25, m.PM10)
	}

	levelChar := fmt.Sprintf("z%d", level)
	args["aqiIcon"] = b.getAQIIcon(level, mcfg.AQIStandard)
	args["aqiVal"] = aqi
	args["aqiLevel"] = int(level)
	// Manual title case for std and levelChar
	stdTitle := "Eu"
	if len(std) > 0 {
		stdTitle = strings.ToUpper(std[:1]) + std[1:]
	}
	levelTitle := strings.ToUpper(levelChar[:1]) + levelChar[1:]
	
	key := "aqiName" + levelTitle + stdTitle
	args["aqiName"] = b.T(chatID, key)
	args["aqiStandardFlag"] = "{icoFlag" + strings.ToUpper(std) + "}"

	log.Debug().Interface("args", args).Msg("tgbot: measurement args built")

	if m.Temperature != 0 {
		args["labelT"] = b.T(chatID, "msgTemp")
		args["valT"] = b.convertTemp(m.Temperature, chatID)
		args["unitT"] = b.unitTempLabel(chatID)
	}
	if m.Humidity != 0 {
		args["labelH"] = b.T(chatID, "msgHum")
		args["valH"] = m.Humidity
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			args["labelDp"] = b.T(chatID, "msgDewPoint")
			args["valDp"] = b.convertTemp(dp, chatID)
		}
	}
	if m.Pressure != 0 {
		args["labelP"] = b.T(chatID, "msgPress")
		args["valP"] = b.convertPress(m.Pressure, chatID)
		args["unitP"] = b.unitPressLabel(chatID)
	}

	return args
}

func (b *Bot) formatMeasurement(chatID int64, m *monitor.Measurement) string {
	args := b.buildMeasurementArgs(chatID, m)
	return b.T(chatID, "msgStatus", args)
}
