// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file implements internalization (i18n) by loading translations, icons,
// and colors, and resolving templates with conditional placeholders.
package tgbot

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/rs/zerolog/log"
)

var (
	langDicts map[string]map[string]string
	iconsMap  map[string]string
	colorsMap map[string]string
	i18nMu    sync.RWMutex
)

func init() {
	langDicts = make(map[string]map[string]string)
	iconsMap = make(map[string]string)
	for k, v := range defaultIcons {
		iconsMap[k] = v
	}
	colorsMap = make(map[string]string)
	for k, v := range defaultColors {
		colorsMap[k] = v
	}

	langDicts["en"] = make(map[string]string)

	execPath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(execPath)
	}
	resDir := filepath.Join(baseDir, "res")

	loadExternalTranslations(resDir)
	loadIcons(resDir)
	loadColors(resDir)
	loadAQIStandards(resDir)
}

var defaultIcons = map[string]string{
	"icoAqi":          "🌬️",
	"icoStatus":       "📊",
	"icoSettings":     "⚙️",
	"icoHistory":      "📜",
	"icoChart":        "📈",
	"icoSubscribe":    "➕",
	"icoUnsubscribe":  "➖",
	"icoBack":         "⬅️",
	"icoBackSettings": "🛠️",
	"icoReset":        "🔄",
	"icoInfo":         "ℹ️",
	"icoSuccess":      "✅",
	"icoError":        "❌",
	"icoWarning":      "⚠️",
	"icoAlert":        "🚨",
	"icoLoud":         "🔊",
	"icoSilent":       "🔕",
	"icoEmpty":        "📁",
	"icoUnknown":      "❓",
	"icoDate":         "📅",
	"icoTime":         "🕒",
	"icoTemp":         "🌡️",
	"icoHum":          "💧",
	"icoPress":        "⏲️",
	"icoDewPoint":     "💦",
	"icoPm10":         "💨",
	"icoPm25":         "░",
	"icoTrendUp":      "📈",
	"icoTrendDown":    "📉",
	"icoTrendFlat":    "➖",
	"icoPollution":    "🌫️",
	"icoChecked":      "☑️",
	"icoUnchecked":    "🔳",
	"icoBullet":       "•",
	"icoThreshold":    "⚖️",
	"icoSetByAQI":     "🧭",
	"icoWrite":        "✍️",
	"icoPlant":        "🌱",
	"icoDevice":       "📡",
	"icoList":         "📋",
	"icoLang":         "🌐",
	"icoDelete":       "🗑️",
	"icoFlagEU":       "🇪🇺",
	"icoFlagUS":       "🇺🇸",
	"icoDynamics":     "↗️",
	"icoLevels":       "📶",

	"icoPmLevel1": "🟩",
	"icoPmLevel2": "🟨",
	"icoPmLevel3": "🟥",

	"icoAqiUSLevel1": "🟢",
	"icoAqiUSLevel2": "🟡",
	"icoAqiUSLevel3": "🟠",
	"icoAqiUSLevel4": "🔴",
	"icoAqiUSLevel5": "🟣",
	"icoAqiUSLevel6": "🟤",
	"icoAqiUSLevel7": "⚫",

	"icoAqiEULevel1": "🔵",
	"icoAqiEULevel2": "🟢",
	"icoAqiEULevel3": "🟡",
	"icoAqiEULevel4": "🟠",
	"icoAqiEULevel5": "🔴",
	"icoAqiEULevel6": "🟣",
}

var defaultColors = map[string]string{
	"colorRed":        "#80090799",
	"colorBlue":       "#0c4c8084",
	"colorPurple":     "#6d197c8c",
	"colorGray":       "#505050",
	"colorGreen":      "#00E400",
	"colorLightBlue":  "#52B6E6",
	"colorYellow":     "#FFFF00",
	"colorOrange":     "#FF7E00",
	"colorDarkRed":    "#FF0000",
	"colorViolet":     "#8F3F97",
	"colorMaroon":     "#7E0023",
	"colorGreenZone":  "#00FF0055",
	"colorYellowZone": "#FFFF0055",
	"colorRedZone":    "#FF000055",
}

func loadExternalTranslations(dir string) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".json" {
			if info.Name() == "ico.json" || info.Name() == "colors.json" || info.Name() == "aqi.json" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				log.Warn().Err(err).Str("path", path).Msg("i18n: failed to read translation file")
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
				log.Info().Str("file", path).Msg("i18n: loaded translations from additional file")
			} else {
				log.Warn().Err(err).Str("path", path).Msg("i18n: failed to unmarshal translation file")
			}
		}
		return nil
	})
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("i18n: failed to walk translations directory")
	}
}

func loadIcons(dir string) {
	path := filepath.Join(dir, "ico.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("i18n: failed to read icons file")
		return
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		i18nMu.Lock()
		for k, v := range m {
			iconsMap[k] = v
		}
		i18nMu.Unlock()
		log.Info().Str("file", path).Msg("i18n: loaded icons from additional file")
	} else {
		log.Warn().Err(err).Str("path", path).Msg("i18n: failed to unmarshal icons file")
	}
}

func loadColors(dir string) {
	path := filepath.Join(dir, "colors.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("i18n: failed to read colors file")
		return
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		i18nMu.Lock()
		for k, v := range m {
			colorsMap[k] = v
		}
		i18nMu.Unlock()
		log.Info().Str("file", path).Msg("i18n: loaded colors from additional file")
	} else {
		log.Warn().Err(err).Str("path", path).Msg("i18n: failed to unmarshal colors file")
	}
}

// loadAQIStandards reads AQI standard definitions from res/aqi.json,
// loads them into sensor.Standards, and applies localized names/icons
// from the currently loaded translation and icon dictionaries.
func loadAQIStandards(dir string) {
	path := filepath.Join(dir, "aqi.json")
	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("i18n: failed to read AQI standards file")
		return
	}

	if err := sensor.LoadStandards(data); err != nil {
		log.Error().Err(err).Msg("i18n: failed to load AQI standards")
		return
	}
	log.Info().Str("file", path).Msg("i18n: loaded AQI standards from additional file")

	// Localize standards from loaded translations/icons.
	// Use GetStandards() to get a thread-safe snapshot.
	stds := sensor.GetStandards()

	i18nMu.Lock()
	defer i18nMu.Unlock()

	for tag, std := range stds {
		tagUpper := strings.ToUpper(tag)

		// 1. Localize Flag
		flagKey := fmt.Sprintf("icoFlag%s", tagUpper)
		if icon, ok := iconsMap[flagKey]; ok {
			std.Flag = icon
		}

		// 2. Localize Standard Names
		stdKey := "standard" + strings.Title(strings.ToLower(tag))
		if name, ok := langDicts["en"][stdKey]; ok {
			std.NameFull = name
		}
		shortKey := "txtStandard" + strings.Title(strings.ToLower(tag))
		if name, ok := langDicts["en"][shortKey]; ok {
			std.NameShort = name
		}

		// 3. Localize Zones
		for i := range std.Zones {
			level := std.Zones[i].Level

			// Zone Name (e.g. aqiNameL1Eu)
			nameKey := fmt.Sprintf("aqiNameL%d%s", level, strings.Title(strings.ToLower(tag)))
			if name, ok := langDicts["en"][nameKey]; ok {
				std.Zones[i].Name = name
			}

			// Zone Icon (e.g. icoAqiEULevel1)
			iconKey := fmt.Sprintf("icoAqi%sLevel%d", tagUpper, level)
			if icon, ok := iconsMap[iconKey]; ok {
				std.Zones[i].Icon = icon
			}
		}
	}
}

func ReloadAll() {
	execPath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(execPath)
	}
	resDir := filepath.Join(baseDir, "res")

	loadExternalTranslations(resDir)
	loadIcons(resDir)
	loadColors(resDir)
	loadAQIStandards(resDir)
}

func AvailableLanguages() []string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	seen := map[string]bool{"en": true}
	langs := []string{"en"}
	execPath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(execPath)
	}
	dir := filepath.Join(baseDir, "res")

	// Language code pattern: "xx" or "xx-XX"
	langRegex := regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			code := strings.TrimSuffix(info.Name(), ".json")
			if langRegex.MatchString(code) {
				if !seen[code] {
					seen[code] = true
					langs = append(langs, code)
				}
			}
		}
		return nil
	})

	sort.Strings(langs)
	return langs
}

func resolveTemplateLocked(lang string, text string, argsMap map[string]interface{}, depth int) string {
	if depth > 50 {
		return text
	}

	for i := 0; i < 20; i++ {
		var result strings.Builder
		resolvedSomething := false

		for j := 0; j < len(text); j++ {
			if text[j] == '{' {
				if j+1 < len(text) && text[j+1] == '{' {
					result.WriteString("{{")
					j++
					continue
				}

				idxOpen := j
				idxClose := -1
				stack := 0
				for k := j; k < len(text); k++ {
					if text[k] == '{' {
						stack++
					} else if text[k] == '}' {
						stack--
						if stack == 0 {
							idxClose = k
							break
						}
					}
				}

				if idxClose != -1 {
					match := text[idxOpen : idxClose+1]
					if strings.HasPrefix(match, "{?") || strings.HasPrefix(match, "{ ?") {
						content := match[2 : len(match)-1]
						parts := splitByTopLevelPercent(content, 3)
						if len(parts) > 0 {
							condition := strings.TrimPrefix(strings.TrimSpace(parts[0]), "?")
							trueText := ""
							if len(parts) > 1 {
								trueText = parts[1]
							}
							falseText := ""
							if len(parts) > 2 {
								falseText = parts[2]
							}

							isTrue := evaluateCondition(argsMap, condition)
							branchText := falseText
							if isTrue {
								branchText = trueText
							}

							if strings.HasPrefix(strings.TrimSpace(branchText), "?") && !strings.HasPrefix(strings.TrimSpace(branchText), "{") {
								branchText = "{" + branchText + "}"
							}

							condResult := resolveTemplateLocked(lang, branchText, argsMap, depth+1)
							result.WriteString(condResult)
							j = idxClose
							resolvedSomething = true
							continue
						}
					}

					// Placeholder with potential format and color
					content := match[1 : len(match)-1]
					sepIdx := -1
					d := 0
					for k := 0; k < len(content); k++ {
						if content[k] == '{' {
							d++
						} else if content[k] == '}' {
							d--
						} else if content[k] == '%' && d == 0 {
							sepIdx = k
							break
						}
					}

					key := content
					format := ""
					if sepIdx != -1 {
						key = strings.TrimSpace(content[:sepIdx])
						format = content[sepIdx+1:]
					}

					resolved, ok := resolvePlaceholderLocked(lang, key, format, argsMap, depth)
					if ok {
						result.WriteString(resolved)
						j = idxClose
						resolvedSomething = true
						continue
					}
				}
			}
			result.WriteByte(text[j])
		}

		text = result.String()
		if !resolvedSomething {
			break
		}
	}

	text = strings.ReplaceAll(text, "{{", "{")
	text = strings.ReplaceAll(text, "}}", "}")
	return text
}

func resolvePlaceholderLocked(lang, key, format string, argsMap map[string]interface{}, depth int) (string, bool) {
	if argsMap != nil {
		if val, exists := argsMap[key]; exists {
			var res string
			if t, okT := val.(time.Time); okT {
				f := format
				if f == "" {
					if dict, okD := langDicts[lang]; okD {
						if defF, okF := dict["format_"+key]; okF {
							f = defF
						} else if defF, okF := dict["formatDatetime"]; okF {
							f = defF
						}
					}
				}
				if f == "" {
					f = "2006-01-02 15:04:05"
				}
				res = t.Local().Format(f)
			} else if format != "" {
				switch format {
				case "toUpper":
					res = strings.ToUpper(fmt.Sprintf("%v", val))
				case "toLower":
					res = strings.ToLower(fmt.Sprintf("%v", val))
				case "toTitle":
					res = strings.Title(strings.ToLower(fmt.Sprintf("%v", val)))
				case "raw":
					return fmt.Sprintf("%v", val), true
				default:
					res = fmt.Sprintf("%"+format, val)
				}
			} else {
				res = fmt.Sprintf("%v", val)
			}

			return html.EscapeString(res), true
		}
	}

	if dict, ok := langDicts[lang]; ok {
		if v, exists := dict[key]; exists {
			return resolveTemplateLocked(lang, v, argsMap, depth+1), true
		}
	}
	if enDict, ok := langDicts["en"]; ok {
		if v, exists := enDict[key]; exists {
			return resolveTemplateLocked(lang, v, argsMap, depth+1), true
		}
	}
	if v, ok := iconsMap[key]; ok {
		return v, true
	}

	return "", false
}

// evaluateCondition parses and evaluates a simple condition expression (e.g. "{var} == value") against the provided arguments map.
func evaluateCondition(argsMap map[string]interface{}, condition string) bool {
	if condition == "" {
		return false
	}
	parts := strings.Fields(condition)
	if len(parts) == 0 {
		return false
	}

	varName := strings.Trim(parts[0], "{}")
	val, ok := argsMap[varName]

	if len(parts) == 1 {
		if !ok || val == nil {
			return varName == "true"
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
			return varName == ""
		}
		if s, isStr := val.(string); isStr {
			return s == ""
		}
		return false
	}
	if operator == "isNotEmpty" {
		if !ok || val == nil {
			return varName != ""
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
	var valStr string
	if ok && val != nil {
		valStr = fmt.Sprintf("%v", val)
	} else {
		valStr = varName
	}

	switch operator {
	case "==", "eq":
		return valStr == target
	case "!=", "ne":
		return valStr != target
	case ">", "gt":
		return compareNumeric(val, varName, target) > 0
	case "<", "lt":
		return compareNumeric(val, varName, target) < 0
	case ">=", "ge":
		return compareNumeric(val, varName, target) >= 0
	case "<=", "le":
		return compareNumeric(val, varName, target) <= 0
	}
	return false
}

// compareNumeric safely compares an interface value (or its raw string form) against a target numeric string.
func compareNumeric(val interface{}, rawVal string, target string) int {
	var f1 float64
	if val != nil {
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
	} else {
		fmt.Sscanf(rawVal, "%f", &f1)
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

// splitByTopLevelPercent splits a string by the '%' character, ignoring any '%' characters that appear within balanced curly braces.
func splitByTopLevelPercent(s string, max int) []string {
	var parts []string
	var current strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			depth++
			current.WriteByte('{')
		} else if s[i] == '}' {
			if depth > 0 {
				depth--
			}
			current.WriteByte('}')
		} else if s[i] == '%' && depth == 0 && (max <= 0 || len(parts) < max-1) {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteByte(s[i])
		}
	}
	parts = append(parts, current.String())
	return parts
}

func (b *Bot) detectLang(langCode string) string {
	for _, l := range AvailableLanguages() {
		if strings.HasPrefix(langCode, l) {
			return l
		}
	}
	return "en"
}

func (b *Bot) TLang(lang, key string, args ...interface{}) string {
	if lang == "" {
		lang = "en"
	}
	i18nMu.RLock()
	defer i18nMu.RUnlock()

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
		if v, ok := iconsMap[key]; ok {
			tmpl = v
			found = true
		}
	}
	if !found {
		return fmt.Sprintf("!!%s!!", key)
	}

	var argsMap map[string]interface{}
	if len(args) == 1 {
		if m, okMap := args[0].(map[string]interface{}); okMap {
			argsMap = m
		}
	}
	return resolveTemplateLocked(lang, tmpl, argsMap, 0)
}

func (b *Bot) I(key string) string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	if v, ok := iconsMap[key]; ok {
		return v
	}
	return ""
}

func (b *Bot) C(key string) string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	if v, ok := colorsMap[key]; ok {
		return v
	}
	return ""
}

func (b *Bot) Resolve(s string) string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	return resolveTemplateLocked("en", s, nil, 0)
}

func GetResolvedLanguageDict(lang string) map[string]string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()

	dict := make(map[string]string)
	if lang == "" {
		lang = "en"
	}

	// Load base English translations
	if enDict, ok := langDicts["en"]; ok {
		for k, v := range enDict {
			dict[k] = resolveTemplateLocked(lang, v, nil, 0)
		}
	}

	// Override with target language
	if lang != "en" {
		if langDict, ok := langDicts[lang]; ok {
			for k, v := range langDict {
				dict[k] = resolveTemplateLocked(lang, v, nil, 0)
			}
		}
	}

	return dict
}
