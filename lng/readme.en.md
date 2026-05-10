# Localization and Placeholder System Documentation

The system is based on recursive resolution of placeholders using the format @key%format@.

---

## 1. Atomic Placeholders (Data from Code)
These are primary values provided by the bot's core logic.

| Placeholder | Type | Description |
| :--- | :--- | :--- |
| @val25@ / @val10@ | Float | Current PM2.5 / PM10 value. |
| @curr25@ / @prev25@ | Float | Current and previous values. |
| @diff25Percent@ | Float | Percentage change. |
| @aqiVal@ | Float | Calculated AQI index. |
| @deviceId@ | String | Device ID (e.g., 12345). |
| @deviceName@ | String | Device name. |
| @date@ | Time | Date object. Requires format. |
| @time@ | Time | Time object. Requires format. |
| @unitPm@ | String | PM unit (µg/m³). |
| @valT@ / @unitT@ | Float/Str | Temperature and unit. |
| @valH@ | Float | Humidity (%). |
| @valP@ / @unitP@ | Float/Str | Pressure and unit. |
| @valDp@ | Float | Dew point. |

---

## 2. Meta-holders (JSON Structure)
Meta-holders are the keys defined in localization files.

### 2.1. Icons
Any key from ico.json is available directly as @icoName@ (e.g., @icoStatus@, @icoAqi@).

### 2.2. Message Templates (Example msgStatus)
The main status message is now assembled from independent text blocks (txt):

"msgStatus": "@icoStatus@ <b>Latest received values</b>\n@txtDateTime@\n\n@txtAqi@\n\n@txtPm25@\n\n@txtPm10@\n\n@txtOtherUnits@\n\n@txtDevice@"

| Key Group (JSON) | Description |
| :--- | :--- |
| msgStatus | Main status message template. |
| txtDateTime | Date and time block. |
| txtAqi | AQI index block. |
| txtPm25 / txtPm10 | PM indicators with dynamics and zones. |
| txtOtherUnits | Meteorological data block. |
| txtDevice | Device identification block. |

---

## 3. Placeholder to Icon Mapping

| Placeholder | Icons used from ico.json |
| :--- | :--- |
| @icon@ | AQI level circles: icoGreen, icoYellow, icoOrange, icoRed... |
| @zone25Icon@ | PM zone squares: icoGreenSq, icoYellowSq, icoRedSq. |
| @trend25Icon@ | Trends: icoTrendUp, icoTrendDown, icoTrendFlat. |

---

## 4. Date and Time Formatting
Supports Go format: %02.01.2006 (date), %15:04:05 (time).
