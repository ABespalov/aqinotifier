package tgbot

import (
	"fmt"
	"strings"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
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
func (b *Bot) getEventHeader(chatID int64, id string) (string, string) {

	if id == "aqi_z1" || strings.HasSuffix(id, "-gd") {
		return icoPlant, b.T(chatID, "msgNorma")
	}

	if strings.HasSuffix(id, "-yd") || strings.HasSuffix(id, "-rd") {
		return icoInfo, b.T(chatID, "msgInfo")
	}

	return icoAlert, b.T(chatID, "msgAlert")
}
func (b *Bot) getEventDescription(chatID int64, id string) string {
	pm10 := "@alert_pm10@"
	pm25 := "@alert_pm25@"
	pms := "@alert_pms@"

	zAccG := "@alert_short_zone_acc_g@"
	zAccY := "@alert_short_zone_acc_y@"
	zAccR := "@alert_short_zone_acc_r@"
	zPreG := "@alert_short_zone_pre_g@"
	zPreY := "@alert_short_zone_pre_y@"
	zPreR := "@alert_short_zone_pre_r@"

	riseIn := func(pm, zone string) string {
		return b.T(chatID, "alertPmRiseIn", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	fallIn := func(pm, zone string) string {
		return b.T(chatID, "alertPmFallIn", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	riseTo := func(pm, zone string) string {
		return b.T(chatID, "alertPmRiseTo", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	fallTo := func(pm, zone string) string {
		return b.T(chatID, "alertPmFallTo", map[string]interface {
		}{"pm": pm, "zone": zone})
	}
	norm := func(pm, zone string) string {
		return b.T(chatID, "alertPmReturn", map[string]interface {
		}{"pm": pm, "zone": zone})
	}

	switch id {

	case "val10-yu":
		return riseTo(pm10, zAccY)
	case "val10-ru":
		return riseTo(pm10, zAccR)
	case "val10-yd":
		return fallTo(pm10, zAccY)
	case "val10-gd":
		return norm(pm10, zAccG)

	case "val25-yu":
		return riseTo(pm25, zAccY)
	case "val25-ru":
		return riseTo(pm25, zAccR)
	case "val25-yd":
		return fallTo(pm25, zAccY)
	case "val25-gd":
		return norm(pm25, zAccG)

	case "vals-yu":
		return riseTo(pms, zAccY)
	case "vals-ru":
		return riseTo(pms, zAccR)
	case "vals-yd":
		return fallTo(pms, zAccY)
	case "vals-gd":
		return norm(pms, zAccG)

	case "diff10-gu":
		return riseIn(pm10, zPreG)
	case "diff10-yu":
		return riseIn(pm10, zPreY)
	case "diff10-ru":
		return riseTo(pm10, zAccR)
	case "diff10-gd":
		return norm(pm10, zAccG)
	case "diff10-yd":
		return fallTo(pm10, zAccY)
	case "diff10-rd":
		return fallIn(pm10, zPreR)

	case "diff25-gu":
		return riseIn(pm25, zPreG)
	case "diff25-yu":
		return riseIn(pm25, zPreY)
	case "diff25-ru":
		return riseTo(pm25, zAccR)
	case "diff25-gd":
		return norm(pm25, zAccG)
	case "diff25-yd":
		return fallTo(pm25, zAccY)
	case "diff25-rd":
		return fallIn(pm25, zPreR)

	case "diffs-gu":
		return riseIn(pms, zPreG)
	case "diffs-yu":
		return riseIn(pms, zPreY)
	case "diffs-ru":
		return riseTo(pms, zAccR)
	case "diffs-gd":
		return norm(pms, zAccG)
	case "diffs-yd":
		return fallTo(pms, zAccY)
	case "diffs-rd":
		return fallIn(pms, zPreR)
	}

	if strings.HasPrefix(id, "aqi_") {
		levelChar := strings.TrimPrefix(id, "aqi_")
		mcfg := b.GetUserSettings(chatID)
		std := strings.ToLower(mcfg.AQIStandard)
		name := b.T(chatID, "aqi_name_"+levelChar+"_"+std)

		if id == "aqi_z1" {
			return b.T(chatID, "alertAqiReturn")
		}

		return b.T(chatID, "alertAqiRise", map[string]interface {
		}{"zone": name})
	}

	return ""
}
func (b *Bot) getAQIIcon(level sensor.AQILevel, standard string) string {
	if strings.ToUpper(standard) == "US" {
		switch level {
		case sensor.LevelGood:
			return icoGreen
		case sensor.LevelModerate:
			return icoYellow
		case sensor.LevelSlightlyUnhealthy:
			return icoOrange
		case sensor.LevelUnhealthy:
			return icoRed
		case sensor.LevelVeryUnhealthy:
			return icoPurple
		case sensor.LevelHazardous:
			return icoMaroon
		case sensor.LevelExtremelyHazardous:
			return icoBlack
		}
	} else {
		switch level {
		case sensor.LevelGood:
			return icoBlue
		case sensor.LevelModerate:
			return icoGreen
		case sensor.LevelSlightlyUnhealthy:
			return icoYellow
		case sensor.LevelUnhealthy:
			return icoOrange
		case sensor.LevelVeryUnhealthy:
			return icoRed
		case sensor.LevelHazardous, sensor.LevelExtremelyHazardous:
			return icoMaroon
		}
	}
	return icoUnknown
}
func (b *Bot) formatDeviceStatus(chatID int64, deviceID string) string {
	m := b.monitor.LastMeasurement(deviceID)
	if m == nil {
		return b.TDevice(chatID, "msgStatusNoData", deviceID)
	}
	return b.formatMeasurement(chatID, m)
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
func (b *Bot) formatDeviceID(chatID int64, deviceID string) string {
	mcfg := b.GetUserSettings(chatID)
	name, ok := mcfg.DeviceNames[deviceID]
	if ok && name != "" {
		return fmt.Sprintf("%s %s (<code>%s</code>)", icoDevice, name, deviceID)
	}
	return fmt.Sprintf("%s %s <code>%s</code>", icoDevice, b.T(chatID, "msgDevice"), deviceID)
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
	var aqi float64
	var level sensor.AQILevel
	if mcfg.AQIStandard == "US" {
		aqi, level = sensor.CalculateUS_AQI(m.PM25, m.PM10)
	} else {
		aqi, level = sensor.CalculateEU_AQI(m.PM25, m.PM10)
	}

	levelChar := fmt.Sprintf("z%d", level)
	aqiName := b.T(chatID, "aqi_name_"+levelChar+"_"+std)

	args := map[string]interface{}{
		"date": m.Timestamp.Local(),
		"time": m.Timestamp.Local(),

		"valAqi":      aqi,
		"valAqiLevel": aqiName,
		"icoAqiLevel": b.getAQIIcon(level, mcfg.AQIStandard),
		"icoAqiFlag":  "@flag_" + std + "@",

		"txtPm25Name": b.T(chatID, "txtLabelPm25"),
		"valPm25":     m.PM25,
		"valPm25Prev": m.PM25Prev,
		"valPm25Diff": m.PM25 - m.PM25Prev,
		"unitPm":      b.T(chatID, "txtChartUnitPm"),

		"txtPm10Name": b.T(chatID, "txtLabelPm10"),
		"valPm10":     m.PM10,
		"valPm10Prev": m.PM10Prev,
		"valPm10Diff": m.PM10 - m.PM10Prev,

		"deviceId": m.DeviceID,
	}

	if name, ok := mcfg.DeviceNames[m.DeviceID]; ok && name != "" {
		args["deviceName"] = name
	}

	if m.PM25Diff != nil {
		args["valPm25DiffPrc"] = *m.PM25Diff
	} else {
		args["valPm25DiffPrc"] = 0.0
	}
	if m.PM10Diff != nil {
		args["valPm10DiffPrc"] = *m.PM10Diff
	} else {
		args["valPm10DiffPrc"] = 0.0
	}

	diff25 := m.PM25 - m.PM25Prev
	if diff25 > 0 {
		args["picPm25Trend"] = icoTrendUp
	} else if diff25 < 0 {
		args["picPm25Trend"] = icoTrendDown
	} else {
		args["picPm25Trend"] = icoTrendFlat
	}

	diff10 := m.PM10 - m.PM10Prev
	if diff10 > 0 {
		args["picPm10Trend"] = icoTrendUp
	} else if diff10 < 0 {
		args["picPm10Trend"] = icoTrendDown
	} else {
		args["picPm10Trend"] = icoTrendFlat
	}

	getZoneIcon := func(v, g, y float64) string {
		if v <= g {
			return icoGreenSq
		}
		if v <= y {
			return icoYellowSq
		}
		return icoRedSq
	}
	args["picPm25Zone"] = getZoneIcon(m.PM25, mcfg.PM25Green, mcfg.PM25Yellow)
	args["picPm10Zone"] = getZoneIcon(m.PM10, mcfg.PM10Green, mcfg.PM10Yellow)

	if m.Temperature != 0 {
		args["valTemp"] = b.convertTemp(m.Temperature, chatID)
		args["unitTemp"] = b.unitTempLabel(chatID)
	}
	if m.Humidity != 0 {
		args["valHumid"] = m.Humidity
		if m.Temperature != 0 {
			dp := CalcDewPoint(m.Temperature, m.Humidity)
			args["valDew"] = b.convertTemp(dp, chatID)
		}
	}
	if m.Pressure != 0 {
		args["valPressure"] = b.convertPress(m.Pressure, chatID)
		args["unitPressure"] = b.unitPressLabel(chatID)
	}

	return args
}

func (b *Bot) formatMeasurement(chatID int64, m *monitor.Measurement) string {
	args := b.buildMeasurementArgs(chatID, m)
	return b.T(chatID, "msgStatus", args)
}
