package tgbot

var (
	IconAQI          = "🌬️"
	IconStatus       = "📊"
	IconSettings     = "⚙️"
	IconHistory      = "📜"
	IconChart        = "📈"
	IconSubscribe    = "➕"
	IconUnsubscribe  = "➖"
	IconBack         = "⬅️"
	IconBackSettings = "🛠️"
	IconReset        = "🔄"
	IconInfo         = "ℹ️"
	IconSuccess      = "✅"
	IconError        = "❌"
	IconWarning      = "⚠️"
	IconAlert        = "🚨"
	IconLoud         = "🔊"
	IconSilent       = "🔕"
	IconEmpty        = "📁"
	IconUnknown      = "❓"
	IconDate         = "📅"
	IconTime         = "🕒"
	IconTemp         = "🌡️"
	IconHum          = "💧"
	IconPress        = "⏲️"
	IconDewPoint     = "💦"
	IconPM10         = "💨"
	IconPM25         = "░"
	IconTrendUp      = "📈"
	IconTrendDown    = "📉"
	IconTrendFlat    = "➖"
	IconPollution    = "🌫️"
	IconChecked      = "☑️"
	IconUnchecked    = "🔳"
	IconBullet       = "•"
	IconGreen        = "🟢"
	IconYellow       = "🟡"
	IconOrange       = "🟠"
	IconRed          = "🔴"
	IconPurple       = "🟣"
	IconMaroon       = "🟤"
	IconBlue         = "🔵"
	IconBlack        = "⚫"
	IconGreenSq      = "🟩"
	IconYellowSq     = "🟨"
	IconRedSq        = "🟥"
	IconThreshold    = "⚖️"
	IconSetByAQI     = "🧭"
	IconWrite        = "✍️"
	IconPlant        = "🌱"
	IconDevice       = "📡"
	IconList         = "📋"
	IconLang         = "🌐"
	IconDelete       = "🗑️"
	IconFlagEU       = "🇪🇺"
	IconFlagUS       = "🇺🇸"
	IconDynamics     = "↗️"
	IconLevels       = "📶"
)

func updateIconVars(m map[string]string) {
	if v, ok := m["IconAQI"]; ok { IconAQI = v }
	if v, ok := m["IconStatus"]; ok { IconStatus = v }
	if v, ok := m["IconSettings"]; ok { IconSettings = v }
	if v, ok := m["IconHistory"]; ok { IconHistory = v }
	if v, ok := m["IconChart"]; ok { IconChart = v }
	if v, ok := m["IconSubscribe"]; ok { IconSubscribe = v }
	if v, ok := m["IconUnsubscribe"]; ok { IconUnsubscribe = v }
	if v, ok := m["IconBack"]; ok { IconBack = v }
	if v, ok := m["IconBackSettings"]; ok { IconBackSettings = v }
	if v, ok := m["IconReset"]; ok { IconReset = v }
	if v, ok := m["IconInfo"]; ok { IconInfo = v }
	if v, ok := m["IconSuccess"]; ok { IconSuccess = v }
	if v, ok := m["IconError"]; ok { IconError = v }
	if v, ok := m["IconWarning"]; ok { IconWarning = v }
	if v, ok := m["IconAlert"]; ok { IconAlert = v }
	if v, ok := m["IconLoud"]; ok { IconLoud = v }
	if v, ok := m["IconSilent"]; ok { IconSilent = v }
	if v, ok := m["IconEmpty"]; ok { IconEmpty = v }
	if v, ok := m["IconUnknown"]; ok { IconUnknown = v }
	if v, ok := m["IconDate"]; ok { IconDate = v }
	if v, ok := m["IconTime"]; ok { IconTime = v }
	if v, ok := m["IconTemp"]; ok { IconTemp = v }
	if v, ok := m["IconHum"]; ok { IconHum = v }
	if v, ok := m["IconPress"]; ok { IconPress = v }
	if v, ok := m["IconDewPoint"]; ok { IconDewPoint = v }
	if v, ok := m["IconPM10"]; ok { IconPM10 = v }
	if v, ok := m["IconPM25"]; ok { IconPM25 = v }
	if v, ok := m["IconTrendUp"]; ok { IconTrendUp = v }
	if v, ok := m["IconTrendDown"]; ok { IconTrendDown = v }
	if v, ok := m["IconTrendFlat"]; ok { IconTrendFlat = v }
	if v, ok := m["IconPollution"]; ok { IconPollution = v }
	if v, ok := m["IconChecked"]; ok { IconChecked = v }
	if v, ok := m["IconUnchecked"]; ok { IconUnchecked = v }
	if v, ok := m["IconBullet"]; ok { IconBullet = v }
	if v, ok := m["IconGreen"]; ok { IconGreen = v }
	if v, ok := m["IconYellow"]; ok { IconYellow = v }
	if v, ok := m["IconOrange"]; ok { IconOrange = v }
	if v, ok := m["IconRed"]; ok { IconRed = v }
	if v, ok := m["IconPurple"]; ok { IconPurple = v }
	if v, ok := m["IconMaroon"]; ok { IconMaroon = v }
	if v, ok := m["IconBlue"]; ok { IconBlue = v }
	if v, ok := m["IconBlack"]; ok { IconBlack = v }
	if v, ok := m["IconGreenSq"]; ok { IconGreenSq = v }
	if v, ok := m["IconYellowSq"]; ok { IconYellowSq = v }
	if v, ok := m["IconRedSq"]; ok { IconRedSq = v }
	if v, ok := m["IconThreshold"]; ok { IconThreshold = v }
	if v, ok := m["IconSetByAQI"]; ok { IconSetByAQI = v }
	if v, ok := m["IconWrite"]; ok { IconWrite = v }
	if v, ok := m["IconPlant"]; ok { IconPlant = v }
	if v, ok := m["IconDevice"]; ok { IconDevice = v }
	if v, ok := m["IconList"]; ok { IconList = v }
	if v, ok := m["IconLang"]; ok { IconLang = v }
	if v, ok := m["IconDelete"]; ok { IconDelete = v }
	if v, ok := m["IconFlagEU"]; ok { IconFlagEU = v }
	if v, ok := m["IconFlagUS"]; ok { IconFlagUS = v }
	if v, ok := m["IconDynamics"]; ok { IconDynamics = v }
	if v, ok := m["IconLevels"]; ok { IconLevels = v }
}
