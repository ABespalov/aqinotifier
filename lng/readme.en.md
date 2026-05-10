# Localization System Documentation (v2.2)

The localization system is based on a powerful **recursive block template engine**, allowing all display logic (icons, conditions, inflections) to be moved from Go code to JSON files.

---

## 1. Template Syntax

Basic placeholder format: `@key%format@`

### 1.1. Case Formatters
- `%toUpper`: CONVERTS TO UPPER CASE
- `%toLower`: converts to lower case
- `%toTitle`: Capitalizes The First Letter

### 1.2. Recursive Blocks (Block Architecture)
You can nest keys within other keys for consistency.

**Example:**
- `actionRise`: `Rise`
- `evtVal10Yu`: `@actionRise@ of @labelPm10@ to @labelZoneYellow@ zone`

### 1.3. Numeric and Time Formatting
- Standard Go formats: `%.1f`, `%d`, `%02.01.2006` etc.

---

## 2. Conditional Logic (Logic)

Syntax: `@?condition%true_text%false_text@`

### 2.1. Comparison Operators
- `==`, `eq`, `!=`, `ne`, `>`, `gt`, `<`, `lt`, `>=`, `le`, `isEmpty`, `isNotEmpty`.

### 2.2. Nested Conditionals (Switch)
If the third part (else) starts with `?`, it is treated as a nested condition.
**Example:** `@?isAlert%A%?isNorma%B%C@@`
