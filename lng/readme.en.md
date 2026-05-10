# Localization System Documentation (v2.0)

The system is based on a **recursive block-based template engine**. The UI construction logic has been moved from Go code to JSON files.

---

## 1. Core Principle: Block Structure
Instead of assembling messages in code, we use "master templates" that reference other sub-templates (placeholders).

**Example (msgStatus):**
```json
"msgStatus": "@icoStatus@ <b>Latest values</b>\n@txtDateTime@\n\n@txtAqi@\n\n@txtPm25@\n\n@txtDevice@"
```
When calling `T("msgStatus")`, the engine automatically resolves `txtDateTime`, `txtAqi`, etc., from the same file.

---

## 2. Atomic Data (from Go)
Primary values provided by the bot. They serve as the foundation for all blocks.

| Placeholder | Description |
| :--- | :--- |
| @aqiVal@ | AQI Index (number). |
| @val25@ / @val10@ | Current PM values (µg/m³). |
| @diff25Percent@ | Change in % (e.g., +15.2). |
| @trend25Icon@ | Trend icon (icoTrendUp/Down). |
| @zone25Icon@ | Zone icon (icoGreenSq/YellowSq/RedSq). |
| @deviceId@ | Device ID. |
| @deviceName@ | Device name. |
| @date@ / @time@ | Time objects (require format, e.g., @date%02.01.2006@). |

---

## 3. File Organization (Sorting)
For maintainability, keys in `ru.json` / `en.json` are sorted **topically**:
1. **Main Templates (msg...)**: Key messages (Status, Notify).
2. **Sub-templates (txt...)**: Blocks used within messages (Date, AQI, PM).
3. **Buttons (btn...)**: Keyboard buttons.
4. **Icons (ico...)**: Icon reference list.
5. **System**: Units, formats, zone names.

---

## 4. Formatting Rules
1. **HTML Tags**: Use native `<b>`, `<i>`, `<code>`.
2. **Unicode**: Unicode escaping (like `\u003cb\u003e`) is forbidden. Use raw text and tags only.
3. **Icons**: All icons use the `ico` prefix (e.g., `@icoAqi@`).
