# AQI Notifier Localization System (v2.2)

The localization system is based on a powerful **recursive block template engine**, allowing all display logic (icons, conditions, inflections) to be moved from Go code to JSON files.

---

## 1. Template Syntax

Basic placeholder format: `@key%format@`

### 1.1. Case Formatters
Used to manipulate the case of the inserted value:
- `%toUpper`: CONVERTS TO UPPER CASE
- `%toLower`: converts to lower case
- `%toTitle`: Capitalizes The First Letter Of Each Word

**Example:** `"Sharp @actionRise%toLower@"` (where `actionRise` = "Rise") -> "Sharp rise".

### 1.2. Numeric Formatting
Uses standard Go `fmt.Printf` syntax:
- `%.1f`: one decimal place (12.3)
- `%.2f`: two decimal places (12.34)
- `%d`: integer

### 1.3. Date and Time Formatting
Uses standard Go `2006-01-02 15:04:05` format:
- `%02.01.2006`: DD.MM.YYYY (02.05.2024)
- `%15:04`: HH:MM (14:30)

---

## 2. Conditional Logic

Syntax: `@?condition%true_text%false_text@`

### 2.1. Comparison Operators
- `==`, `eq`: Equals
- `!=`, `ne`: Not equals
- `>`, `gt`: Greater than
- `<`, `lt`: Less than
- `>=`, `le`: Greater or equal
- `isEmpty`: Check for empty string
- `isNotEmpty`: Check for presence of value

### 2.2. Nested Conditionals (Switch)
If the third part (else) starts with `?`, it is treated as a nested condition.
**Example:** `@?isAlert%A%?isNorma%B%C@@` (If Alert - A, else if Norma - B, else C).

---

## 3. Data Mapping (Go -> JSON)

Below are the main keys called from the code and the data passed to them.

### 3.1. `msgStatus` (Device Status)
Called when requesting current readings.
**Passed variables:**
- `@date@`, `@time@`: Measurement time.
- `@aqiVal@`, `@aqiLevel@`: AQI value and level (1-7).
- `@aqiName@`: Localized level name (e.g., "Good").
- `@aqiStandardFlag@`: Standard flag icon (EU/US).
- `@val25@`, `@val10@`: Current PM values.
- `@diff25Percent@`, `@diff10Percent@`: Change in percent.
- `@trend25Icon@`, `@trend10Icon@`: Trend icon (up/down/flat).
- `@zone25Icon@`, `@zone10Icon@`: Current zone icon (colored square).
- `@deviceName@`, `@deviceId@`: Device information.
- `@valT@`, `@valH@`, `@valP@`: Temperature, humidity, pressure (if available).

### 3.2. `msgAlertNotify` (Event Notification)
Called when alerts are triggered.
**Additional variables (in addition to the list above):**
- `@isAlert@`: `true` if entering a danger zone.
- `@isNorma@`: `true` if returning to normal.
- `@evt*@`: Specific event flags (e.g., `@evtVal10Yu@`, `@evtPm25Rise@`). 
  If the event is active, the variable is set to `true`.

---

## 4. Development Rules

1. **No Logic in Code**: Icons, titles, and alert lists must be constructed within JSON using `@?condition%...%...@`.
2. **Key Names**: Always use `camelCase` for new keys.
3. **Unicode**: Do not use Unicode escaping (`\u003cb\u003e`). Use native HTML: `<b>`, `<i>`, etc.
4. **Sorting**: Maintain thematic grouping of keys. Master templates (`msg*`) should always be at the top.
