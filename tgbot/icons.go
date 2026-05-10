package tgbot

var (
	icoAqi          = "🌬️"
	icoStatus       = "📊"
	icoSettings     = "⚙️"
	icoHistory      = "📜"
	icoChart        = "📈"
	icoSubscribe    = "➕"
	icoUnsubscribe  = "➖"
	icoBack         = "⬅️"
	icoBackSettings = "🛠️"
	icoReset        = "🔄"
	icoInfo         = "ℹ️"
	icoSuccess      = "✅"
	icoError        = "❌"
	icoWarning      = "⚠️"
	icoAlert        = "🚨"
	icoLoud         = "🔊"
	icoSilent       = "🔕"
	icoEmpty        = "📁"
	icoUnknown      = "❓"
	icoDate         = "📅"
	icoTime         = "🕒"
	icoTemp         = "🌡️"
	icoHum          = "💧"
	icoPress        = "⏲️"
	icoDewPoint     = "💦"
	icoPm10         = "💨"
	icoPm25         = "░"
	icoTrendUp      = "📈"
	icoTrendDown    = "📉"
	icoTrendFlat    = "➖"
	icoPollution    = "🌫️"
	icoChecked      = "☑️"
	icoUnchecked    = "🔳"
	icoBullet       = "•"
	icoGreen        = "🟢"
	icoYellow       = "🟡"
	icoOrange       = "🟠"
	icoRed          = "🔴"
	icoPurple       = "🟣"
	icoMaroon       = "🟤"
	icoBlue         = "🔵"
	icoBlack        = "⚫"
	icoGreenSq      = "🟩"
	icoYellowSq     = "🟨"
	icoRedSq        = "🟥"
	icoThreshold    = "⚖️"
	icoSetByAQI     = "🧭"
	icoWrite        = "✍️"
	icoPlant        = "🌱"
	icoDevice       = "📡"
	icoList         = "📋"
	icoLang         = "🌐"
	icoDelete       = "🗑️"
	icoFlagEU       = "🇪🇺"
	icoFlagUS       = "🇺🇸"
	icoDynamics     = "↗️"
	icoLevels       = "📶"

	icoAqiUSLevel1 = "🟢"
	icoAqiUSLevel2 = "🟡"
	icoAqiUSLevel3 = "🟠"
	icoAqiUSLevel4 = "🔴"
	icoAqiUSLevel5 = "🟣"
	icoAqiUSLevel6 = "🟤"
	icoAqiUSLevel7 = "⚫"

	icoAqiEULevel1 = "🔵"
	icoAqiEULevel2 = "🟢"
	icoAqiEULevel3 = "🟡"
	icoAqiEULevel4 = "🟠"
	icoAqiEULevel5 = "🔴"
	icoAqiEULevel6 = "🟣"
)

func updateicoVars(m map[string]string) {
	if v, ok := m["icoAqi"]; ok {
		icoAqi = v
	}
	if v, ok := m["icoStatus"]; ok {
		icoStatus = v
	}
	if v, ok := m["icoSettings"]; ok {
		icoSettings = v
	}
	if v, ok := m["icoHistory"]; ok {
		icoHistory = v
	}
	if v, ok := m["icoChart"]; ok {
		icoChart = v
	}
	if v, ok := m["icoSubscribe"]; ok {
		icoSubscribe = v
	}
	if v, ok := m["icoUnsubscribe"]; ok {
		icoUnsubscribe = v
	}
	if v, ok := m["icoBack"]; ok {
		icoBack = v
	}
	if v, ok := m["icoBackSettings"]; ok {
		icoBackSettings = v
	}
	if v, ok := m["icoReset"]; ok {
		icoReset = v
	}
	if v, ok := m["icoInfo"]; ok {
		icoInfo = v
	}
	if v, ok := m["icoSuccess"]; ok {
		icoSuccess = v
	}
	if v, ok := m["icoError"]; ok {
		icoError = v
	}
	if v, ok := m["icoWarning"]; ok {
		icoWarning = v
	}
	if v, ok := m["icoAlert"]; ok {
		icoAlert = v
	}
	if v, ok := m["icoLoud"]; ok {
		icoLoud = v
	}
	if v, ok := m["icoSilent"]; ok {
		icoSilent = v
	}
	if v, ok := m["icoEmpty"]; ok {
		icoEmpty = v
	}
	if v, ok := m["icoUnknown"]; ok {
		icoUnknown = v
	}
	if v, ok := m["icoDate"]; ok {
		icoDate = v
	}
	if v, ok := m["icoTime"]; ok {
		icoTime = v
	}
	if v, ok := m["icoTemp"]; ok {
		icoTemp = v
	}
	if v, ok := m["icoHum"]; ok {
		icoHum = v
	}
	if v, ok := m["icoPress"]; ok {
		icoPress = v
	}
	if v, ok := m["icoDewPoint"]; ok {
		icoDewPoint = v
	}
	if v, ok := m["icoPm10"]; ok {
		icoPm10 = v
	}
	if v, ok := m["icoPm25"]; ok {
		icoPm25 = v
	}
	if v, ok := m["icoTrendUp"]; ok {
		icoTrendUp = v
	}
	if v, ok := m["icoTrendDown"]; ok {
		icoTrendDown = v
	}
	if v, ok := m["icoTrendFlat"]; ok {
		icoTrendFlat = v
	}
	if v, ok := m["icoPollution"]; ok {
		icoPollution = v
	}
	if v, ok := m["icoChecked"]; ok {
		icoChecked = v
	}
	if v, ok := m["icoUnchecked"]; ok {
		icoUnchecked = v
	}
	if v, ok := m["icoBullet"]; ok {
		icoBullet = v
	}
	if v, ok := m["icoGreen"]; ok {
		icoGreen = v
	}
	if v, ok := m["icoYellow"]; ok {
		icoYellow = v
	}
	if v, ok := m["icoOrange"]; ok {
		icoOrange = v
	}
	if v, ok := m["icoRed"]; ok {
		icoRed = v
	}
	if v, ok := m["icoPurple"]; ok {
		icoPurple = v
	}
	if v, ok := m["icoMaroon"]; ok {
		icoMaroon = v
	}
	if v, ok := m["icoBlue"]; ok {
		icoBlue = v
	}
	if v, ok := m["icoBlack"]; ok {
		icoBlack = v
	}
	if v, ok := m["icoGreenSq"]; ok {
		icoGreenSq = v
	}
	if v, ok := m["icoYellowSq"]; ok {
		icoYellowSq = v
	}
	if v, ok := m["icoRedSq"]; ok {
		icoRedSq = v
	}
	if v, ok := m["icoThreshold"]; ok {
		icoThreshold = v
	}
	if v, ok := m["icoSetByAQI"]; ok {
		icoSetByAQI = v
	}
	if v, ok := m["icoWrite"]; ok {
		icoWrite = v
	}
	if v, ok := m["icoPlant"]; ok {
		icoPlant = v
	}
	if v, ok := m["icoDevice"]; ok {
		icoDevice = v
	}
	if v, ok := m["icoList"]; ok {
		icoList = v
	}
	if v, ok := m["icoLang"]; ok {
		icoLang = v
	}
	if v, ok := m["icoDelete"]; ok {
		icoDelete = v
	}
	if v, ok := m["icoFlagEU"]; ok {
		icoFlagEU = v
	}
	if v, ok := m["icoFlagUS"]; ok {
		icoFlagUS = v
	}
	if v, ok := m["icoDynamics"]; ok {
		icoDynamics = v
	}
	if v, ok := m["icoLevels"]; ok {
		icoLevels = v
	}

	if v, ok := m["icoAqiUSLevel1"]; ok { icoAqiUSLevel1 = v }
	if v, ok := m["icoAqiUSLevel2"]; ok { icoAqiUSLevel2 = v }
	if v, ok := m["icoAqiUSLevel3"]; ok { icoAqiUSLevel3 = v }
	if v, ok := m["icoAqiUSLevel4"]; ok { icoAqiUSLevel4 = v }
	if v, ok := m["icoAqiUSLevel5"]; ok { icoAqiUSLevel5 = v }
	if v, ok := m["icoAqiUSLevel6"]; ok { icoAqiUSLevel6 = v }
	if v, ok := m["icoAqiUSLevel7"]; ok { icoAqiUSLevel7 = v }

	if v, ok := m["icoAqiEULevel1"]; ok { icoAqiEULevel1 = v }
	if v, ok := m["icoAqiEULevel2"]; ok { icoAqiEULevel2 = v }
	if v, ok := m["icoAqiEULevel3"]; ok { icoAqiEULevel3 = v }
	if v, ok := m["icoAqiEULevel4"]; ok { icoAqiEULevel4 = v }
	if v, ok := m["icoAqiEULevel5"]; ok { icoAqiEULevel5 = v }
	if v, ok := m["icoAqiEULevel6"]; ok { icoAqiEULevel6 = v }
}
