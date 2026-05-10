package tgbot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	langDicts        map[string]map[string]string
	iconsMap         map[string]string
	i18nMu           sync.RWMutex
	placeholderRegex = regexp.MustCompile(`@([a-zA-Z0-9_]+)(?:%([^@]+))?@`)
)

func init() {
	langDicts = make(map[string]map[string]string)
	iconsMap = make(map[string]string)

	langDicts["en"] = make(map[string]string)
	for k, v := range fallbackEN {
		langDicts["en"][k] = v
	}

	loadExternalTranslations("lng")
	loadIcons("lng")
}

func loadExternalTranslations(dir string) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".json" {
			if info.Name() == "ico.json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			var dict map[string]string
			if err := json.Unmarshal(data, &dict); err == nil {
				lang := strings.TrimSuffix(info.Name(), ".json")
				i18nMu.Lock()
				if langDicts[lang] == nil {
					langDicts[lang] = make(map[string]string)
				}
				for k, v := range dict {
					langDicts[lang][k] = v
				}
				i18nMu.Unlock()
			} else {
				fmt.Printf("i18n: failed to unmarshal %s: %v\n", path, err)
			}
		}
		return nil
	})
}

func loadIcons(dir string) {
	path := filepath.Join(dir, "ico.json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("i18n: failed to read %s: %v\n", path, err)
		return
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		i18nMu.Lock()
		for k, v := range m {
			iconsMap[k] = v
		}
		i18nMu.Unlock()
		updateicoVars(m)
	} else {
		fmt.Printf("i18n: failed to unmarshal %s: %v\n", path, err)
	}
}

// ReloadAll reloads translations and icons from the lng/ directory.
func ReloadAll() {
	loadExternalTranslations("lng")
	loadIcons("lng")
}

// AvailableLanguages returns a sorted list of language codes found in the lng/ directory.
func AvailableLanguages() []string {
	seen := map[string]bool{"en": true}
	langs := []string{"en"}
	_ = filepath.Walk("lng", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".json" && info.Name() != "ico.json" {
			code := strings.TrimSuffix(filepath.Base(path), ".json")
			if !seen[code] {
				seen[code] = true
				langs = append(langs, code)
			}
		}
		return nil
	})
	return langs
}

func resolveTemplate(lang string, text string, argsMap map[string]interface{}, depth int) string {
	if depth > 15 {
		return text // Prevent infinite recursion
	}

	text = strings.ReplaceAll(text, "%%", "%%%%") // Protect double percent signs (though not strictly needed if we process conditional first)

	// Evaluate conditionals first or after? 
	// If we do placeholders first, they might break the conditional regex if they insert @ or %.
	// It's safer to resolve placeholders first, because conditionals might depend on the replaced values,
	// BUT conditionals might wrap placeholders. 
	// Wait! If placeholders are resolved first, `@?deviceName isNotEmpty%@deviceName@ (%` 
	// becomes `@?deviceName isNotEmpty%MySensor (%`. This is PERFECT.
	
	text = placeholderRegex.ReplaceAllStringFunc(text, func(match string) string {
		submatch := placeholderRegex.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		key := submatch[1]
		// Ignore if it's a conditional start
		if strings.HasPrefix(match, "@?") {
			return match
		}
		format := ""
		if len(submatch) > 2 {
			format = submatch[2]
		}

		// 1. Try argsMap
		if argsMap != nil {
			if val, ok := argsMap[key]; ok {
				var valStr string
				if t, okT := val.(time.Time); okT {
					f := format
					if f == "" {
						i18nMu.RLock()
						if dict, okD := langDicts[lang]; okD {
							if defF, okF := dict["format_"+key]; okF {
								f = defF
							} else if defF, okF := dict["formatDatetime"]; okF {
								f = defF
							}
						}
						i18nMu.RUnlock()
					}
					if f == "" {
						f = "2006-01-02 15:04:05"
					}
					valStr = t.Format(f)
				} else if format != "" {
					valStr = fmt.Sprintf("%"+format, val)
				} else {
					valStr = fmt.Sprintf("%v", val)
				}
				return resolveTemplate(lang, valStr, argsMap, depth+1)
			}
		}

		// 2. Try langDicts and iconsMap
		i18nMu.RLock()
		dict, ok := langDicts[lang]
		var tmpl string
		found := false
		if ok {
			tmpl, found = dict[key]
		}
		if !found {
			if enDict, okEN := langDicts["en"]; okEN {
				tmpl, found = enDict[key]
			}
		}
		if !found {
			tmpl, found = iconsMap[key]
		}
		i18nMu.RUnlock()

		if found {
			return resolveTemplate(lang, tmpl, argsMap, depth+1)
		}

		return match // Keep unresolved
	})

	// 3. Process conditionals manually to support nesting: @?var op val%true%false@
	for {
		start := strings.Index(text, "@?")
		if start == -1 {
			break
		}

		// Find matching @ accounting for nested @? ... @
		end := -1
		stack := 0
		for i := start; i < len(text); i++ {
			if i+1 < len(text) && text[i:i+2] == "@?" {
				stack++
				i++
			} else if text[i] == '@' {
				stack--
				if stack == 0 {
					end = i
					break
				}
			}
		}

		if end == -1 {
			// Unbalanced @?, just remove it to prevent infinite loop
			text = text[:start] + text[start+2:]
			continue
		}

		content := text[start+2 : end]
		parts := splitByTopLevelPercent(content)

		if len(parts) < 1 {
			text = text[:start] + text[end+1:]
			continue
		}

		condition := strings.TrimSpace(parts[0])
		trueText := ""
		if len(parts) > 1 {
			trueText = parts[1]
		}
		falseText := ""
		if len(parts) > 2 {
			falseText = parts[2]
		}

		isTrue := evaluateCondition(argsMap, condition)
		result := ""
		if isTrue {
			result = resolveTemplate(lang, trueText, argsMap, depth+1)
		} else {
			if strings.HasPrefix(falseText, "?") {
				falseText = "@" + falseText + "@"
			}
			result = resolveTemplate(lang, falseText, argsMap, depth+1)
		}

		text = text[:start] + result + text[end+1:]
	}

	text = strings.ReplaceAll(text, "%%%%", "%")
	return text
}

func evaluateCondition(argsMap map[string]interface{}, condition string) bool {
	if argsMap == nil {
		return false
	}

	parts := strings.Fields(condition)
	if len(parts) == 0 {
		return false
	}

	varName := parts[0]
	val, ok := argsMap[varName]

	if len(parts) == 1 {
		if !ok || val == nil {
			return false
		}
		if b, isBool := val.(bool); isBool {
			return b
		}
		if s, isStr := val.(string); isStr {
			return s != "" && s != "false"
		}
		return true
	}

	operator := parts[1]

	if operator == "isEmpty" {
		if !ok || val == nil {
			return true
		}
		if s, isStr := val.(string); isStr {
			return s == ""
		}
		return false
	}
	if operator == "isNotEmpty" {
		if !ok || val == nil {
			return false
		}
		if s, isStr := val.(string); isStr {
			return s != ""
		}
		return true
	}

	if len(parts) < 3 {
		return false
	}

	target := strings.Join(parts[2:], " ")
	valStr := fmt.Sprintf("%v", val)

	switch operator {
	case "==", "eq":
		return valStr == target
	case "!=", "ne":
		return valStr != target
	case ">", "gt":
		return compareNumeric(val, target) > 0
	case "<", "lt":
		return compareNumeric(val, target) < 0
	case ">=", "ge":
		return compareNumeric(val, target) >= 0
	case "<=", "le":
		return compareNumeric(val, target) <= 0
	}

	return false
}

func compareNumeric(val interface{}, target string) int {
	var f1 float64
	switch v := val.(type) {
	case float64:
		f1 = v
	case int:
		f1 = float64(v)
	case int64:
		f1 = float64(v)
	default:
		fmt.Sscanf(fmt.Sprintf("%v", val), "%f", &f1)
	}

	var f2 float64
	fmt.Sscanf(target, "%f", &f2)

	if f1 > f2 {
		return 1
	}
	if f1 < f2 {
		return -1
	}
	return 0
}

func splitByTopLevelPercent(s string) []string {
	var parts []string
	var current strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i:i+2] == "@?" {
			depth++
			current.WriteString("@?")
			i++
		} else if s[i] == '@' {
			depth--
			current.WriteByte('@')
		} else if s[i] == '%' && depth == 0 {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteByte(s[i])
		}
	}
	parts = append(parts, current.String())
	return parts
}

// T returns a localized string for the given key and chat ID.
func (b *Bot) T(chatID int64, key string, args ...interface{}) string {
	lang := b.store.GetLanguage(chatID)
	return b.TLang(lang, key, args...)
}

// TDevice is a helper that automatically injects deviceId and deviceName into the template arguments.
func (b *Bot) TDevice(chatID int64, key string, deviceID string, args ...map[string]interface{}) string {
	m := make(map[string]interface{})
	if len(args) > 0 {
		for k, v := range args[0] {
			m[k] = v
		}
	}
	mcfg := b.GetUserSettings(chatID)
	m["deviceId"] = deviceID
	if name, ok := mcfg.DeviceNames[deviceID]; ok {
		m["deviceName"] = name
	}
	return b.T(chatID, key, m)
}

func (b *Bot) detectLang(langCode string) string {
	if strings.HasPrefix(langCode, "ru") {
		return "ru"
	}
	return "en"
}

// TLang returns a localized string for a specific language code.
func (b *Bot) TLang(lang, key string, args ...interface{}) string {
	if lang == "" {
		lang = "en"
	}

	i18nMu.RLock()
	dict, ok := langDicts[lang]
	var tmpl string
	found := false
	if ok {
		tmpl, found = dict[key]
	}
	if !found {
		if enDict, okEN := langDicts["en"]; okEN {
			tmpl, found = enDict[key]
		}
	}
	i18nMu.RUnlock()

	if !found {
		return fmt.Sprintf("!!%s!!", key)
	}

	var argsMap map[string]interface{}
	if len(args) == 1 {
		if m, okMap := args[0].(map[string]interface{}); okMap {
			argsMap = m
		}
	}

	resolved := resolveTemplate(lang, tmpl, argsMap, 0)

	if len(args) > 0 && argsMap == nil {
		return fmt.Sprintf(resolved, args...)
	}
	return resolved
}

// I returns an icon by its key from ico.json.
func (b *Bot) I(key string) string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	if v, ok := iconsMap[key]; ok {
		return v
	}
	return ""
}

// fallbackEN contains the hardcoded English localization as a final safety net.
var fallbackEN = map[string]string{

	"alertPm10":                   "PM10",
	"alertPm25":                   "PM2.5",
	"alertPms":                    "PM2.5 & PM10",
	"alertAqiCleanShort":        "AQI level returned to normal",
	"alertActionUp":              "Rise",
	"alertActionDown":            "Fall",
	"alertAqiFull":               "<b>AQI: %s</b> (%s %s)\nAQI: <b>%.1f</b>",
	"alertAqiClean":              "%s <b>AQI level returned to normal</b> (%s %s)\nAQI: <b>%.1f</b>",
	"alertAqiShort":              "AQI Level: %s %s",

	"btnAqiSettings":             "%s AQI Settings",
	"btnAqiStandard":             "Standard: %s %s",
	"btnChartAqi":                "%s AQI Chart",
	"btnChartHum":                "%s Humidity",
	"btnChartPm":                 "%s PM2.5 + PM10",
	"btnChartPress":              "%s Pressure",
	"btnChartTemp":               "%s Temperature",
	"btnCharts":                   "%s Charts 24h",
	"btnHistory":                  "%s History",
	"btnList":                     "%s Your Subscriptions",
	"btnMainMenu":                "%s Main Menu",
	"btnPm10Green":               "%s PM10 Green",
	"btnPm10Yellow":              "%s PM10 Yellow",
	"btnPm25Green":               "%s PM2.5 Green",
	"btnPm25Yellow":              "%s PM2.5 Yellow",
	"btnPm10Diff":                "%s Dynamics PM10",
	"btnPm25Diff":                "%s Dynamics PM2.5",
	"btnSettings":                 "%s Settings",
	"btnBack":                     "%s Back",
	"btnSilentProfiles":          "%s PM Dynamics",
	"btnSoundProfiles":           "%s PM Levels",
	"btnStatus":                   "%s Status",
	"btnSubscribe":                "%s Subscribe",
	"btnMonSettings":             "%s Monitoring Settings",
	"btnThresholds":               "%s Pollution Zones",
	"btnUnsubscribe":              "%s Unsubscribe",
	"btnSetByAqi":               "%s Set by AQI",
	"btnResetDefaults":           "%s Default Values",
	"txtChartPmTitle":               "PM2.5 and PM10",
	"txtChartUnitPm":                "µg/m³",
	"txtChartSubjectPm":             "PM2.5 & PM10",
	"txtChartSubjectAqi":            "AQI",
	"txtChartSubjectTemp":           "temperature and dew point",
	"txtChartSubjectHum":            "humidity",
	"txtChartSubjectPress":          "pressure",
	"txtChartScalePm10":             "PM10 Scale",
	"txtChartScalePm25":             "PM2.5 Scale",
	"txtCmdHelpDesc":                "Main menu and help",
	"txtCmdLangDesc":                "Language and units",
	"txtCmdMenuDesc":                "Main menu",
	"txtCmdListDesc":                "My subscriptions / Management",
	"txtCmdStartDesc":               "Start bot / Help",
	"txtCmdStatusDesc":              "Current device status",
	"langEn":                      "English",
	"langRu":                      "Russian",
	"txtLabelPm10":                   "PM10",
	"txtLabelPm25":                   "PM2.5",
	"txtLabelZone1":                 "Green",
	"txtLabelZone2":                 "Yellow",
	"txtLabelZoneSuffix":            "zone",
	"txtLabelDynamics":               "Dynamics",
	"msgAlert":                    "WARNING",
	"msgAlreadySub":              "%s You are already subscribed to device <code>%s</code>",
	"btnYes":                      "%s Yes",
	"btnNo":                       "%s No",
	"btnCancel":                   "%s Cancel",
	"msgResetConfirm":            "%s <b>Are you sure you want to reset all settings to defaults?</b>\n\n",
	"msgResetConfirmDetails":    "The following values will be set:\n\n%s <b>%s %s:</b> %.1f / %.1f (%s %.1f%%)\n%s <b>%s %s:</b> %.1f / %.1f (%s %.1f%%)\n\n%s <b>AQI Standard:</b> %s %s\n\n%s <b>Units:</b> %s, %s\n\n%s <b>Notifications:</b>\n%s",
	"msgResetDone":               "%s <b>Settings reset!</b>\n\nCurrent parameters:\n%s %s: %s <b>%.1f</b>, %s <b>%.1f</b>, %s <b>%.1f%%</b>\n%s %s: %s <b>%.1f</b>, %s <b>%.1f</b>, %s <b>%.1f%%</b>",
	"msgChartsMenu":              "%s <b>Charts for the last 24 hours</b>\n\nSelect a chart to display.",
	"msgDecrease":                 "LEVEL DECREASE",
	"msgDevice":                   "Device",
	"msgDisabled":                 "disabled",
	"msgEnabled":                  "enabled",
	"msgErrorCharts":             "%s Error generating charts: %v",
	"msgErrorNumber":             "%s Error: please enter a numeric value.",
	"msgErrorPositive":           "%s Error: please enter a positive integer.",
	"msgErrorYellow":             "%s Error: yellow zone value must be greater than or equal to green.",
	"msgErrorSendCh":            "%s Failed to send charts.",
	"msgHelp":                     "%[1]s <b>AQI Notifier Bot</b> v%[3]s\n\nThis bot monitors air quality data from your sensors and sends notifications about zone changes or sudden dynamics.\n\n<b>Main commands:</b>\n/list — list your subscriptions and manage them\n/status — current indicators for all your devices\n/history — history of recent measurements\n/help — main menu and help\n/lang — language and units\n\n<b>Management:</b>\n%[4]s Click <b>Settings -> Subscriptions</b> to add or remove a device.\n%[4]s In the settings section (<b>Settings</b>) you can change zone thresholds (Green/Yellow/Red) and notification types.\n%[4]s Notifications about transitions between zones come with sound. Others (dynamic changes within zones) — silently.",
	"msgHistoryEmpty":            "%s History for device <code>%s</code> is empty.",
	"msgHistoryTitle":            "%s <b>History:</b> <code>%s</code>",
	"msgHistoryFooter":           "%s <b>History of recent %d measurements</b>\n%s",
	"msgChart24hTitle":          "%s <b>%s chart for the last 24 hours</b>\n%s",
	"msgHum":                      "Humidity",
	"msgInvalidDeviceId":        "%s Device ID must contain only digits.",
	"msgAlertsLoudLabel":        "%s <b>Loud Notifications:</b>\n",
	"msgAlertsSilentLabel":      "%s <b>Silent Notifications:</b>\n",
	"msgManageSubs":              "Subscription management:",
	"msgMonSettings":             "%s <b>Monitoring Settings:</b>\n",
	"msgNoSubs":                  "%s You have no active subscriptions.\nClick %s <b>Subscribe</b> to add a device.",
	"msgNorma":                    "NORMAL RESTORED",
	"msgPmInfoFmt":              "%s %s: %s <b>%.1f</b>, %s <b>%.1f</b>, %s <b>%.1f%%</b>\n",
	"msgPress":                    "Pressure",
	"msgPromptDevice":            "%s Enter <b>device ID</b> to subscribe:",
	"msgSelectDevice":            "%s <b>Select a device:</b>",
	"msgSelectHistory":           "%s <b>Select a device to view history:</b>",
	"msgSelectLang":              "%s Select language / Выберите язык:",
	"msgStatusHeader":            "%s <b>Latest received values</b>\n%s %s %s %s\n\n",
	"msgStatusAqi":               "%s <b>AQI Level: %.1f</b> — %s %s",
	"msgLoudAlerts":              "%s <b>PM Level Notification Settings:</b>\n",
	"msgSilentAlerts":            "%s <b>PM Dynamics Notification Settings:</b>\n",
	"msgAqiSettings":             "%s Choose AQI calculation standard and configure notifications.",
	"btnWithSound":               "With sound",
	"btnWithoutSound":            "Without sound",
	"btnInactive":                 "Inactive",
	"btnSetPmByAqi":            "%s Set PM zones by AQI",
	"txtAqiNameZ1Us":               "Good",
	"txtAqiNameZ2Us":               "Moderate",
	"txtAqiNameZ3Us":               "Unhealthy (sensitive groups)",
	"txtAqiNameZ4Us":               "Unhealthy",
	"txtAqiNameZ5Us":               "Very Unhealthy",
	"txtAqiNameZ6Us":               "Hazardous",
	"txtAqiNameZ7Us":               "Extremely Hazardous",
	"txtAqiNameZ1Eu":               "Good",
	"txtAqiNameZ2Eu":               "Fair",
	"txtAqiNameZ3Eu":               "Moderate",
	"txtAqiNameZ4Eu":               "Poor",
	"txtAqiNameZ5Eu":               "Very Poor",
	"txtAqiNameZ6Eu":               "Extreme",
	"txtStandardEu":                  "EU",
	"txtStandardUs":                  "US",

	"msgSubscribed":               "%s You subscribed to device <code>%s</code>",
	"msgTemp":                     "Temperature",
	"msgDewPoint":                "Dew point",

	"msgBoundary":                 "Boundary",
	"msgThresholdLabel":          "Threshold",
	"msgThresholdDiffLabel":     "Dynamics (%)",
	"msgThresholdTitleFmt":      "%[4]s %[1]s %[2]s: %[3]s %[5]s",
	"msgThresholdDiffTitleFmt": "%[3]s %[2]s %[1]s",
	"msgThresholdDiffTitle":     "Tracked dynamic change of",
	"msgThresholdPrompt":         "<b>%s</b>\n\nCurrent value: <b>%.1f</b>\nEnter new numeric value:",
	"msgThresholdUpd":            "%s %s updated from <b>%.1f</b> to <b>%.1f</b>",
	"msgThresholdsMenu":          "%s <b>Pollution Zones</b>\n\n%s <b>%s</b>\n      %s %s %.1f\n      %s %s %.1f\n\n%s <b>%s</b>\n      %s %s %.1f\n      %s %s %.1f\n\nSelect a parameter to change.",
	"msgAqiCycleMenu":           "%s <b>Set zone boundaries by AQI standard</b>\n\n%s <b>%s</b>\n      %s %s %.1f\n      %s %s %.1f\n\n%s <b>%s</b>\n      %s %s %.1f\n      %s %s %.1f",
	"msgToggleSuccess":           "%s %s is now <b>%s</b>",
	"msgUnitMmhg":                "mmHg",
	"msgUnsubscribed":             "%s You unsubscribed from device <code>%s</code>",
	"msgNoChanges":               "No changes",
	"msgYourSubs":                "%s <b>Your subscriptions:</b>\nClick on a device for data or use the buttons below for management.",
	"msg_threshold_desc":           "Changing %s threshold between %s and %s zones",
	"msg_threshold_diff_desc":      "Changing %s dynamics threshold (%%)",
	"msgStatusNoData":               "No data for device <code>%s</code>",
	"unitC":                       "Celsius (°C)",
	"unitF":                       "Fahrenheit (°F)",
	"unitHpa":                     "hPa",
	"unitMmhg":                    "mmHg",
	"msgStatusTime":              "%s %s %s %s",
	"msgStatusPm":                "%s <b>%s: %.2f %s</b> %s\n",
	"msgStatusDiff":              "    %s<b>%+.1f%%</b> (%.2f → %.2f)\n\n",
	"msgStatusTemp":              "%s %s: %.1f %s\n",
	"msgStatusHum":               "%s %s: %.1f%%\n",
	"msgStatusPress":             "%s %s: %.1f %s\n",
	"msgStatusDewPoint":         "%s %s: %.1f %s\n",
	"msgStatusDevice":            "%s %s",
	"msgRenamePrompt":            "%s Enter new name for device <code>%s</code>\n\nTo cancel, click the button below.",
	"msgDeviceRenamed":           "✅ Device renamed: %s (<code>%s</code>)",
	"msgRenameCancel":            "%s Renaming cancelled",
	"btnRename":                   "%s Rename",
	"msgSettingsTitle":           "%s <b>Monitoring & Notification Settings</b>\n\nSelect a section to change parameters.",
	"msgSoundSettings":           "Configure notification types for each event.",
	"alertV10Short":              "PM10",
	"alertV25Short":              "PM2.5",
	"alertVsShort":               "PM10+2.5",
	"alertShortActionUp":        "Growth",
	"alertShortActionDown":      "Decrease",
	"msgInfo":                     "INFORMATION",
	"alertPmRiseIn":             "PM%s level rise in %s",
	"alertPmFallIn":             "PM%s level fall in %s",
	"alertPmRiseTo":             "PM%s level rise to %s",
	"alertPmFallTo":             "PM%s level fall to %s",
	"alertPmReturn":              "PM%s level returned to %s",
	"alertAqiRise":               "AQI level rise to \"%s\" zone",
	"alertAqiFall":               "AQI level fall to \"%s\" zone",
	"alertAqiReturn":             "AQI level returned to normal",
	"alertShortZoneAcc1":       "Green",
	"alertShortZoneAcc2":       "Yellow",
	"alertShortZoneAcc3":       "Red",
	"alertShortZonePre1":       "Green",
	"alertShortZonePre2":       "Yellow",
	"alertShortZonePre3":       "Red",
}
