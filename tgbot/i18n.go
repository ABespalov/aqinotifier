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
	langDicts map[string]map[string]string
	iconsMap  map[string]string
	i18nMu    sync.RWMutex
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

func ReloadAll() {
	loadExternalTranslations("lng")
	loadIcons("lng")
}

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
	if depth > 50 {
		return text
	}

	for i := 0; i < 20; i++ {
		var result strings.Builder
		resolvedSomething := false

		for j := 0; j < len(text); j++ {
			if text[j] == '{' {
				// Check for escaped {{
				if j+1 < len(text) && text[j+1] == '{' {
					result.WriteString("{{")
					j++
					continue
				}

				// Find matching }
				idxOpen := j
				idxClose := -1
				stack := 0
				for k := j; k < len(text); k++ {
					if k+1 < len(text) && text[k:k+2] == "{{" {
						k++
						continue
					}
					if k+1 < len(text) && text[k:k+2] == "}}" {
						k++
						continue
					}
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
					
					// Case A: Conditional
					if strings.HasPrefix(match, "{?") {
						content := match[2 : len(match)-1]
						parts := splitByTopLevelPercent(content)
						if len(parts) > 0 {
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
							var condResult string
							// Auto-wrap nested shortcut conditionals: ?var%true%false
							if strings.HasPrefix(trueText, "?") { trueText = "{" + trueText + "}" }
							if strings.HasPrefix(falseText, "?") { falseText = "{" + falseText + "}" }

							if isTrue {
								condResult = resolveTemplate(lang, trueText, argsMap, depth+1)
							} else {
								condResult = resolveTemplate(lang, falseText, argsMap, depth+1)
							}
							result.WriteString(condResult)
							j = idxClose
							resolvedSomething = true
							continue
						}
					}

					// Case B: Regular placeholder
					innerRegex := regexp.MustCompile(`^\{([^{}%?]+)(?:%([^}]+))?\}$`)
					submatch := innerRegex.FindStringSubmatch(match)
					if submatch != nil {
						key := submatch[1]
						format := ""
						if len(submatch) > 2 {
							format = submatch[2]
						}

						resolved, ok := resolvePlaceholder(lang, key, format, argsMap)
						if ok {
							result.WriteString(resolved)
							j = idxClose
							resolvedSomething = true
							continue
						}
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

	// Final cleanup of escaped braces
	text = strings.ReplaceAll(text, "{{", "{")
	text = strings.ReplaceAll(text, "}}", "}")

	return text
}

func resolvePlaceholder(lang, key, format string, argsMap map[string]interface{}) (string, bool) {
	// 1. Try argsMap
	if argsMap != nil {
		if val, exists := argsMap[key]; exists {
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
				return t.Format(f), true
			} else if format != "" {
				switch format {
				case "toUpper":
					return strings.ToUpper(fmt.Sprintf("%v", val)), true
				case "toLower":
					return strings.ToLower(fmt.Sprintf("%v", val)), true
				case "toTitle":
					return strings.Title(strings.ToLower(fmt.Sprintf("%v", val))), true
				default:
					return fmt.Sprintf("%"+format, val), true
				}
			} else {
				return fmt.Sprintf("%v", val), true
			}
		}
	}

	// 2. Try dictionaries and icons
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	
	if dict, ok := langDicts[lang]; ok {
		if v, exists := dict[key]; exists {
			return v, true
		}
	}
	if enDict, ok := langDicts["en"]; ok {
		if v, exists := enDict[key]; exists {
			return v, true
		}
	}
	if v, ok := iconsMap[key]; ok {
		return v, true
	}

	return "", false
}

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
			return varName != "" && varName != "false"
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

func compareNumeric(val interface{}, rawVal string, target string) int {
	var f1 float64
	if val != nil {
		switch v := val.(type) {
		case float64: f1 = v
		case int: f1 = float64(v)
		case int64: f1 = float64(v)
		default: fmt.Sscanf(fmt.Sprintf("%v", val), "%f", &f1)
		}
	} else {
		fmt.Sscanf(rawVal, "%f", &f1)
	}
	var f2 float64
	fmt.Sscanf(target, "%f", &f2)
	if f1 > f2 { return 1 }
	if f1 < f2 { return -1 }
	return 0
}

func splitByTopLevelPercent(s string) []string {
	var parts []string
	var current strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '{' {
			depth++
			current.WriteByte('{')
		} else if s[i] == '}' {
			if depth > 0 { depth-- }
			current.WriteByte('}')
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

func (b *Bot) T(chatID int64, key string, args ...interface{}) string {
	lang := b.store.GetLanguage(chatID)
	return b.TLang(lang, key, args...)
}

func (b *Bot) TDevice(chatID int64, key string, deviceID string, args ...map[string]interface{}) string {
	m := make(map[string]interface{})
	if len(args) > 0 {
		for k, v := range args[0] {
			m[k] = v
		}
	}
	mcfg := b.GetUserSettings(chatID)
	m["deviceId"] = deviceID
	m["deviceName"] = "" 
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

func (b *Bot) TLang(lang, key string, args ...interface{}) string {
	if lang == "" { lang = "en" }
	i18nMu.RLock()
	dict, ok := langDicts[lang]
	var tmpl string
	found := false
	if ok { tmpl, found = dict[key] }
	if !found {
		if enDict, okEN := langDicts["en"]; okEN {
			tmpl, found = enDict[key]
		}
	}
	i18nMu.RUnlock()
	if !found { return fmt.Sprintf("!!%s!!", key) }
	var argsMap map[string]interface{}
	if len(args) == 1 {
		if m, okMap := args[0].(map[string]interface{}); okMap {
			argsMap = m
		}
	}
	return resolveTemplate(lang, tmpl, argsMap, 0)
}

func (b *Bot) I(key string) string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()
	if v, ok := iconsMap[key]; ok { return v }
	return ""
}

var fallbackEN = map[string]string{
	"msgTemp": "Temperature",
	"msgDewPoint": "Dew point",
}
