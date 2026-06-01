// Package dashboard handles loading layouts, managing visual endpoints,
// preparing telemetry and rendering dashboard screens to EPD, PNG, or BMP.
package dashboard

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/fogleman/gg"
	"golang.org/x/image/bmp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed Tamzen8x16r.ttf
var embeddedFontBytes []byte

var (
	embeddedFont *opentype.Font
)

func init() {
	var err error
	embeddedFont, err = opentype.Parse(embeddedFontBytes)
	if err != nil {
		panic("dashboard: failed to parse embedded Tamzen font: " + err.Error())
	}
}

// RenderContext holds state for drawing elements on the GG context.
type RenderContext struct {
	dc        *gg.Context
	cfg       *Config
	telemetry map[string]interface{}
	history   []monitor.Measurement
	fontCache map[string]font.Face
}

// parseHexColor parses a hex color string (e.g. "#FF0000" or "red") into color.RGBA.
func parseHexColor(s string) (color.RGBA, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 6 {
		var r, g, b uint8
		_, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
		return color.RGBA{R: r, G: g, B: b, A: 255}, err
	}
	return color.RGBA{}, fmt.Errorf("invalid hex color %s", s)
}

// colorDistance calculates Euclidean distance between two colors in RGB space.
func colorDistance(c1, c2 color.RGBA) float64 {
	dr := float64(c1.R) - float64(c2.R)
	dg := float64(c1.G) - float64(c2.G)
	db := float64(c1.B) - float64(c2.B)
	return dr*dr + dg*dg + db*db
}

// parseColor resolves a color reference from the global palette or uses it directly.
func (rc *RenderContext) parseColor(c string) string {
	if c == "" {
		return ""
	}
	if rc.cfg.Screen.Palette != nil {
		if hex, ok := rc.cfg.Screen.Palette[c]; ok {
			return hex
		}
	}
	return c
}

// getFontFace loads a font face from the config or falls back to the embedded Tamzen font.
func (rc *RenderContext) getFontFace(fontName string) font.Face {
	if fontName == "" {
		return nil
	}
	if face, ok := rc.fontCache[fontName]; ok {
		return face
	}
	fontDef, ok := rc.cfg.Fonts[fontName]
	if !ok {
		return nil
	}

	// Try reading file relative to running directory
	fontBytes, err := os.ReadFile(fontDef.File)
	if err != nil {
		// Attempt resolution relative to executable directory
		exe, errExe := os.Executable()
		if errExe == nil {
			altPath := filepath.Join(filepath.Dir(exe), fontDef.File)
			fontBytes, err = os.ReadFile(altPath)
		}
	}

	if err != nil {
		// Gracefully fall back to embedded font face
		face, errFace := opentype.NewFace(embeddedFont, &opentype.FaceOptions{
			Size:    fontDef.Size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if errFace == nil {
			rc.fontCache[fontName] = face
			return face
		}
		return nil
	}

	f, err := opentype.Parse(fontBytes)
	if err != nil {
		face, _ := opentype.NewFace(embeddedFont, &opentype.FaceOptions{
			Size:    fontDef.Size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		return face
	}

	hintStyle := font.HintingFull
	switch strings.ToLower(fontDef.Hinting) {
	case "none":
		hintStyle = font.HintingNone
	case "vertical":
		hintStyle = font.HintingVertical
	case "full":
		hintStyle = font.HintingFull
	}

	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    fontDef.Size,
		DPI:     72,
		Hinting: hintStyle,
	})
	if err != nil {
		face, _ := opentype.NewFace(embeddedFont, &opentype.FaceOptions{
			Size:    fontDef.Size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		return face
	}

	rc.fontCache[fontName] = face
	return face
}

// loadFont applies the loaded font face to the GG context.
func (rc *RenderContext) loadFont(fontName string) {
	face := rc.getFontFace(fontName)
	if face != nil {
		rc.dc.SetFontFace(face)
	}
}

// getStringBounds calculates pixel width/height of a string.
func getStringBounds(face font.Face, s string) (xMin, yMin, xMax, yMax float64) {
	if face == nil || s == "" {
		return 0, 0, 0, 0
	}
	bounds, _ := font.BoundString(face, s)
	xMin = float64(bounds.Min.X) / 64.0
	yMin = float64(bounds.Min.Y) / 64.0
	xMax = float64(bounds.Max.X) / 64.0
	yMax = float64(bounds.Max.Y) / 64.0
	return
}

// parseAlignFraction parses layout ratio fractions like "1/2" or static floats.
func parseAlignFraction(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, "/")
	if len(parts) == 2 {
		num, err1 := strconv.ParseFloat(parts[0], 64)
		den, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil && den != 0 {
			return num / den, true
		}
	} else if len(parts) == 1 {
		num, err := strconv.ParseFloat(parts[0], 64)
		if err == nil {
			return num, true
		}
	}
	return 0, false
}

// parseDashStyle decodes style commands like "dashed.2.14" to dash/gap widths.
func parseDashStyle(style string) (dashW, gapW float64) {
	dashW = 2.0
	gapW = 14.0
	if style == "" {
		return
	}
	parts := strings.Split(style, ".")
	if len(parts) == 3 && parts[0] == "dashed" {
		if dw, err := strconv.ParseFloat(parts[1], 64); err == nil {
			dashW = dw
		}
		if gw, err := strconv.ParseFloat(parts[2], 64); err == nil {
			gapW = gw
		}
	}
	return
}

// cleanBase trims property suffixes to get the core variable name.
func cleanBase(s string) string {
	suffixes := []string{
		".zone.index",
		".zone.name",
		".value.to_celsius",
		".value.to_mm_hg",
		".value.to_usa",
		".value",
		".label",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}

// resolvePlaceholders evaluates text template variables like "{temp.value.to_celsius%+.1f}".
func (rc *RenderContext) resolvePlaceholders(template string) string {
	now := time.Now()
	res := template

	re := regexp.MustCompile(`\{([^{}]+)\}`)
	matches := re.FindAllStringSubmatch(res, -1)
	for _, match := range matches {
		fullMatch := match[0]
		content := match[1]

		if strings.HasPrefix(content, "date:") {
			layout := strings.TrimPrefix(content, "date:")
			res = strings.ReplaceAll(res, fullMatch, now.Local().Format(layout))
			continue
		}
		if strings.HasPrefix(content, "time:") {
			layout := strings.TrimPrefix(content, "time:")
			res = strings.ReplaceAll(res, fullMatch, now.Local().Format(layout))
			continue
		}

		varName := content
		format := "%v"
		if idx := strings.Index(content, "%"); idx != -1 {
			varName = strings.TrimSpace(content[:idx])
			format = strings.TrimSpace(content[idx:])
		}

		if val, ok := rc.telemetry[varName]; ok {
			formatted := fmt.Sprintf(format, val)
			res = strings.ReplaceAll(res, fullMatch, formatted)
		}
	}
	return res
}

// Render evaluates visual YAML layout nodes and renders them using graphics canvas.
func Render(cfg *Config, telemetry map[string]interface{}, history []monitor.Measurement) (image.Image, error) {
	dc := gg.NewContext(cfg.Screen.Width, cfg.Screen.Height)

	rc := &RenderContext{
		dc:        dc,
		cfg:       cfg,
		telemetry: telemetry,
		history:   history,
		fontCache: make(map[string]font.Face),
	}

	// Fill background
	bgHex := rc.parseColor(cfg.Output.Background)
	if bgHex == "" {
		bgHex = "#FFFFFF"
	}
	dc.SetHexColor(bgHex)
	dc.Clear()

	// Apply default element alignments
	for i := range cfg.Layout {
		el := &cfg.Layout[i]
		if el.Align.H == "" {
			el.Align.H = cfg.Defaults.Align.H
		}
		if el.Align.V == "" {
			el.Align.V = cfg.Defaults.Align.V
		}
		if el.Value.Align.H == "" {
			el.Value.Align.H = cfg.Defaults.Align.H
		}
		if el.Value.Align.V == "" {
			el.Value.Align.V = cfg.Defaults.Align.V
		}
		if el.Label.Align.H == "" {
			el.Label.Align.H = cfg.Defaults.Align.H
		}
		if el.Label.Align.V == "" {
			el.Label.Align.V = cfg.Defaults.Align.V
		}
	}

	// Render elements sequentially
	for _, el := range cfg.Layout {
		switch el.Type {
		case "value_card", "value":
			valIntf := telemetry[el.Source]
			valFloat := 0.0
			valStr := ""
			isString := false

			if valIntf != nil {
				if s, ok := valIntf.(string); ok {
					valStr = s
					isString = true
				} else {
					switch v := valIntf.(type) {
					case float64:
						valFloat = v
					case int:
						valFloat = float64(v)
					case int64:
						valFloat = float64(v)
					}
				}
			}

			base := cleanBase(el.Source)
			label := ""
			if lblVal, ok := telemetry[base+".label"]; ok {
				label = fmt.Sprintf("%v", lblVal)
			}
			if label == "" {
				label = el.Label.Template
			}

			unit := ""
			if el.Unit.Parameter != "" {
				// i18n key lookup for units
				unitKey := "unit" + strings.Title(strings.ToLower(el.Unit.Parameter))
				if unitKey == "unitMmhg" {
					unitKey = "unitMmhg"
				}
				if val, ok := telemetry[unitKey]; ok {
					unit = fmt.Sprintf("%v", val)
				}
			}
			if unit == "" {
				if val, ok := telemetry[base+".unit"]; ok {
					unit = fmt.Sprintf("%v", val)
				}
			}

			if !isString {
				fmtFmt := "%.0f"
				if el.Format != "" {
					fmtFmt = el.Format
				}
				valStr = fmt.Sprintf(fmtFmt, valFloat)
			}

			rectX := el.Position.X
			rectY := el.Position.Y
			rectW := el.Size.W
			rectH := el.Size.H

			var valXMin, valYMin, valXMax, valYMax float64
			var hasVal bool
			if el.Value.Font.Face != "" && valStr != "" {
				hasVal = true
				vFace := rc.getFontFace(el.Value.Font.Face)
				valXMin, valYMin, valXMax, valYMax = getStringBounds(vFace, valStr)
			}

			var labelXMin, labelYMin, labelXMax, labelYMax float64
			var hasLabel bool
			if el.Label.Font.Face != "" && label != "" {
				hasLabel = true
				lFace := rc.getFontFace(el.Label.Font.Face)
				labelXMin, labelYMin, labelXMax, labelYMax = getStringBounds(lFace, label)
			}

			var unitXMin, unitYMin, unitXMax, unitYMax float64
			var hasUnit bool
			if el.Unit.Font.Face != "" && unit != "" && el.Unit.Align.P != "none" {
				hasUnit = true
				uFace := rc.getFontFace(el.Unit.Font.Face)
				unitXMin, unitYMin, unitXMax, unitYMax = getStringBounds(uFace, unit)
			}

			// Alignment of label
			var labelX, labelY float64
			if hasLabel {
				switch el.Label.Align.H {
				case "left":
					labelX = rectX - labelXMin
				case "right":
					labelX = rectX + rectW - labelXMax
				default:
					labelX = rectX + rectW/2 - (labelXMin+labelXMax)/2
				}
				labelX += el.Label.Margin.L - el.Label.Margin.R

				switch el.Label.Align.V {
				case "top":
					labelY = rectY - labelYMin
				case "bottom":
					labelY = rectY + rectH - labelYMax
				default:
					labelY = rectY + rectH/2 - (labelYMin+labelYMax)/2
				}
				labelY += el.Label.Margin.T - el.Label.Margin.B
			}

			// Value and Unit layout alignment
			unitPos := el.Unit.Align.P
			if unitPos == "" {
				if el.Unit.Align.V == "under_center" || el.Unit.Align.V == "under" {
					unitPos = "bottom"
				} else {
					unitPos = "right"
				}
			}

			var valX, valY float64
			var unitX, unitY float64
			var rowXMin, rowXMax float64

			if hasVal {
				rowXMin = valXMin
				rowXMax = valXMax

				if hasUnit {
					if unitPos == "bottom" {
						valCenter := (valXMin + valXMax) / 2
						unitCenter := (unitXMin + unitXMax) / 2
						unitX = valCenter - unitCenter
						gapV := el.Unit.Align.S
						unitY = valYMax + gapV - unitYMin

						rowXMin = math.Min(valXMin, unitX+unitXMin)
						rowXMax = math.Max(valXMax, unitX+unitXMax)
					} else {
						gapH := el.Unit.Align.S
						unitX = valXMax + gapH - unitXMin

						uy := 0.0
						fracV, isFracV := parseAlignFraction(el.Unit.Align.V)
						if isFracV {
							valHeight := valYMax - valYMin
							uy = valYMax - valHeight*fracV
						} else {
							switch el.Unit.Align.V {
							case "top":
								uy = valYMin - unitYMin
							case "bottom":
								uy = valYMax - unitYMax
							case "idxUp":
								uy = valYMin
							case "idxDown":
								uy = valYMax
							default:
								uy = (valYMin+valYMax)/2 - (unitYMin+unitYMax)/2
							}
						}
						unitY = uy
						rowXMax = unitX + unitXMax
					}
				}
			} else if hasUnit {
				rowXMin = unitXMin
				rowXMax = unitXMax
				unitX = 0
				unitY = 0
			}

			if hasVal || hasUnit {
				switch el.Value.Align.H {
				case "left":
					valX = rectX - rowXMin
				case "right":
					valX = rectX + rectW - rowXMax
				default:
					valX = rectX + rectW/2 - (rowXMin+rowXMax)/2
				}
				valX += el.Value.Margin.L - el.Value.Margin.R

				if hasVal {
					switch el.Value.Align.V {
					case "top":
						valY = rectY - valYMin
					case "bottom":
						valY = rectY + rectH - valYMax
					default:
						valY = rectY + rectH/2 - (valYMin+valYMax)/2
					}
					valY += el.Value.Margin.T - el.Value.Margin.B
				} else if hasUnit {
					switch el.Value.Align.V {
					case "top":
						valY = rectY - unitYMin
					case "bottom":
						valY = rectY + rectH - unitYMax
					default:
						valY = rectY + rectH/2 - (unitYMin+unitYMax)/2
					}
					valY += el.Value.Margin.T - el.Value.Margin.B
				}
			}

			// Render border background and stroke
			var bX, bY, bW, bH, bR float64
			padding := 0.0
			if el.Border != nil {
				padding = el.Border.Padding
				bR = el.Radius
				if bR <= 0 {
					bR = 8.0
				}

				if len(el.Border.Affects) == 0 {
					bX = rectX - padding
					bY = rectY - padding
					bW = rectW + padding*2
					bH = rectH + padding*2
				} else {
					// Calculate bounds of elements inside the border
					minX, minY := math.MaxFloat64, math.MaxFloat64
					maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
					hasAffected := false

					valLeft := valX + valXMin
					valRight := valX + valXMax
					valTop := valY + valYMin
					valBottom := valY + valYMax

					unitLeft := valX + unitX + unitXMin
					unitRight := valX + unitX + unitXMax
					unitTop := valY + unitY + unitYMin
					unitBottom := valY + unitY + unitYMax

					labelLeft := labelX + labelXMin
					labelRight := labelX + labelXMax
					labelTop := labelY + labelYMin
					labelBottom := labelY + labelYMax

					for _, affected := range el.Border.Affects {
						switch affected {
						case "value":
							if hasVal {
								minX = math.Min(minX, valLeft)
								maxX = math.Max(maxX, valRight)
								minY = math.Min(minY, valTop)
								maxY = math.Max(maxY, valBottom)
								hasAffected = true
							}
							if hasUnit {
								minX = math.Min(minX, unitLeft)
								maxX = math.Max(maxX, unitRight)
								minY = math.Min(minY, unitTop)
								maxY = math.Max(maxY, unitBottom)
								hasAffected = true
							}
						case "label":
							if hasLabel {
								minX = math.Min(minX, labelLeft)
								maxX = math.Max(maxX, labelRight)
								minY = math.Min(minY, labelTop)
								maxY = math.Max(maxY, labelBottom)
								hasAffected = true
							}
						case "unit":
							if hasUnit {
								minX = math.Min(minX, unitLeft)
								maxX = math.Max(maxX, unitRight)
								minY = math.Min(minY, unitTop)
								maxY = math.Max(maxY, unitBottom)
								hasAffected = true
							}
						}
					}

					if hasAffected {
						bX = minX - padding
						bY = minY - padding
						bW = (maxX - minX) + padding*2
						bH = (maxY - minY) + padding*2
					} else {
						bX = rectX - padding
						bY = rectY - padding
						bW = rectW + padding*2
						bH = rectH + padding*2
					}
				}

				bT := getThreshold(telemetry, el.Border.Thresholds)
				bg := rc.parseColor(bT.Bg)
				if bg == "" {
					bg = rc.parseColor(el.Border.Bg)
				}
				fg := rc.parseColor(bT.Fg)
				if fg == "" {
					fg = rc.parseColor(el.Border.Fg)
				}

				if bg != "" {
					dc.SetHexColor(bg)
					dc.DrawRoundedRectangle(bX, bY, bW, bH, bR)
					dc.Fill()
				}
				if fg != "" && el.Border.Thin > 0 {
					dc.SetHexColor(fg)
					dc.SetLineWidth(el.Border.Thin)
					dc.DrawRoundedRectangle(bX, bY, bW, bH, bR)
					dc.Stroke()
				}
			}

			// Render components texts on top of background
			if hasLabel {
				lblT := getThreshold(telemetry, el.Label.Thresholds)
				lblFg := rc.parseColor(lblT.Fg)
				if lblFg == "" {
					lblFg = rc.parseColor(el.Label.Font.Fg)
				}
				if lblFg == "" {
					lblFg = "#000000"
				}

				rc.loadFont(el.Label.Font.Face)
				dc.SetHexColor(lblFg)
				dc.DrawString(label, math.Round(labelX), math.Round(labelY))
			}

			if hasVal {
				valT := getThreshold(telemetry, el.Value.Thresholds)
				valFg := rc.parseColor(valT.Fg)
				if valFg == "" {
					valFg = rc.parseColor(el.Value.Font.Fg)
				}
				if valFg == "" {
					valFg = "#000000"
				}

				rc.loadFont(el.Value.Font.Face)
				dc.SetHexColor(valFg)
				dc.DrawString(valStr, math.Round(valX), math.Round(valY))
			}

			if hasUnit {
				uT := getThreshold(telemetry, el.Unit.Thresholds)
				unitFg := rc.parseColor(uT.Fg)
				if unitFg == "" {
					unitFg = rc.parseColor(el.Unit.Font.Fg)
				}
				if unitFg == "" {
					unitFg = "#000000"
				}

				rc.loadFont(el.Unit.Font.Face)
				dc.SetHexColor(unitFg)
				dc.DrawString(unit, math.Round(valX+unitX), math.Round(valY+unitY))
			}

		case "text":
			text := rc.resolvePlaceholders(el.Template)
			rc.loadFont(el.Font.Face)
			dc.SetHexColor(rc.parseColor(el.Font.Fg))

			var tx float64 = el.Position.X + el.Size.W/2
			var tax float64 = 0.5
			if el.Align.H == "left" {
				tx = el.Position.X
				tax = 0.0
			}

			lines := strings.Split(text, "\n")
			for i, line := range lines {
				// Anchored text drawing helper
				w, h := dc.MeasureString(line)
				lx := tx - tax*w
				ly := el.Position.Y + float64(i)*25 + 15 + (1.0-0.5)*h
				dc.DrawString(line, math.Round(lx), math.Round(ly))
			}

		case "separator", "line":
			dc.SetHexColor(rc.parseColor(el.Font.Fg))
			dc.DrawRectangle(el.Position.X, el.Position.Y, el.Size.W, el.Size.H)
			dc.Fill()

		case "chart":
			numPoints := el.Points
			if numPoints == 0 {
				numPoints = 48
			}

			// Determine history standard
			defaultStd := "US"
			if stdVal, ok := telemetry["aqi_standard"]; ok {
				defaultStd = fmt.Sprintf("%v", stdVal)
			}

			// Compile data points from telemetry history
			var data []float64
			if len(rc.history) > 0 {
				duration := 24 * time.Hour
				if el.Duration != "" {
					if d, err := time.ParseDuration(el.Duration); err == nil {
						duration = d
					}
				}
				data = ResampleHistory(rc.history, duration, numPoints, el.Source, defaultStd)
			} else {
				// Fallback to dynamic sine wave with random noise for POC/Mock if no database history is loaded
				data = make([]float64, numPoints)
				for i := 0; i < numPoints; i++ {
					progress := float64(i) / float64(numPoints-1)
					angle := progress * 3 * math.Pi
					val := 85.0 + 70.0*math.Sin(angle)
					val += -3.0 + rand.Float64()*6.0
					if val < 15 {
						val = 15
					}
					if val > 155 {
						val = 155
					}
					data[i] = val
				}
			}

			// Automatically determine max Y value with safety headroom
			maxVal := 155.0
			for _, v := range data {
				if v > maxVal {
					maxVal = v
				}
			}
			maxVal = math.Ceil(maxVal * 1.05)

			// Draw chart columns
			barW := el.Size.W / float64(numPoints)
			for i, v := range data {
				barH := (v / maxVal) * el.Size.H
				bx1 := el.Position.X + float64(i)*barW
				bx2 := el.Position.X + float64(i+1)*barW
				by := el.Position.Y + el.Size.H - barH

				t := getThreshold(telemetry, el.Thresholds)
				// Fetch matching condition for specific value
				valT := el.Thresholds
				c := ""
				for _, th := range valT {
					// We create a temporary telemetry map with local value of pollutant to run thresholds locally
					localTel := make(map[string]interface{})
					for k, val := range telemetry {
						localTel[k] = val
					}
					localTel[el.Source] = v
					localTel[cleanBase(el.Source)] = v
					if evalCondition(th.Condition, localTel) {
						c = rc.parseColor(th.Bg)
						if c == "" {
							c = rc.parseColor(th.Color)
						}
						break
					}
				}

				if c == "" {
					c = rc.parseColor(t.Bg)
				}
				if c == "" {
					c = rc.parseColor(t.Color)
				}
				if c == "" {
					c = "#000000"
				}

				dc.SetHexColor(c)
				dc.DrawRectangle(bx1, by, bx2-bx1, barH)
				dc.Fill()
			}

			// Draw horizontal threshold guidelines
			for _, tl := range el.ThresholdLines {
				// Evaluate condition relative to target value
				tlValue := tl.Value
				if tlValue == 0 && tl.Condition != "" {
					// If zone-based condition is specified (e.g. "aqi.usa.zone.index == 2")
					// we can extract the threshold breakpoint value from standards!
					parts := strings.Split(tl.Condition, " ")
					if len(parts) == 3 {
						var zone int
						fmt.Sscanf(parts[2], "%d", &zone)
						if zone > 1 {
							baseSource := cleanBase(el.Source)
							if std := sensor.GetStandard(defaultStd); std != nil {
								if strings.Contains(baseSource, "pm25") && zone-2 < len(std.Breakpoints25) {
									tlValue = std.Breakpoints25[zone-2]
								} else if strings.Contains(baseSource, "pm10") && zone-2 < len(std.Breakpoints10) {
									tlValue = std.Breakpoints10[zone-2]
								} else if strings.Contains(baseSource, "aqi") && zone-2 < len(std.IndexPoints) {
									tlValue = std.IndexPoints[zone-2]
								}
							}
						}
					}
				}

				if tlValue == 0 {
					continue
				}

				ly := math.Round(el.Position.Y + el.Size.H - (tlValue/maxVal)*el.Size.H)
				tlColorHex := rc.parseColor(tl.Fg)
				if tlColorHex == "" {
					tlColorHex = rc.parseColor(tl.Color)
				}
				if tlColorHex == "" {
					tlColorHex = rc.parseColor(tl.Bg)
				}
				if tlColorHex == "" {
					tlColorHex = "#000000"
				}

				lineWidth := tl.W
				if lineWidth <= 0 {
					lineWidth = 2.0
				}

				dashW, gapW := parseDashStyle(tl.Style)
				totalPeriod := dashW + gapW

				for cx := el.Position.X; cx < el.Position.X+el.Size.W; cx += totalPeriod {
					cw := dashW
					if cx+cw > el.Position.X+el.Size.W {
						cw = (el.Position.X + el.Size.W) - cx
					}
					if cw <= 0 {
						break
					}

					barIndex := int((cx - el.Position.X) / barW)
					if barIndex < 0 {
						barIndex = 0
					} else if barIndex >= len(data) {
						barIndex = len(data) - 1
					}
					v := data[barIndex]
					barH := (v / maxVal) * el.Size.H
					by := el.Position.Y + el.Size.H - barH

					dotColor := tlColorHex
					if ly >= by {
						// Apply color inversion if drawing inside an identical colored bar
						barColorHex := ""
						for _, th := range el.Thresholds {
							localTel := make(map[string]interface{})
							for k, val := range telemetry {
								localTel[k] = val
							}
							localTel[el.Source] = v
							localTel[cleanBase(el.Source)] = v
							if evalCondition(th.Condition, localTel) {
								barColorHex = rc.parseColor(th.Bg)
								break
							}
						}
						if barColorHex == "" {
							barColorHex = "#000000"
						}
						if dotColor == barColorHex {
							dotColor = "#FFFFFF" // Invert to white for visibility
						}
					}

					dc.SetHexColor(dotColor)
					dc.DrawRectangle(cx, ly-lineWidth/2, cw, lineWidth)
					dc.Fill()
				}
			}

			// Render Axis & Notches
			showYAxis := true
			showYLabels := true
			yStep := 0.0
			var zoneStyles []map[string]string
			yAxisFont := "tiny"
			yAxisFontFg := "black"
			yAxisW := 2.0
			yAxisFg := "black"
			yNotchesVisible := true
			yNotchesFg := "black"
			yNotchesW := 2.0
			yNotchesH := 6.0
			yLabelsCount := 0
			yLabelsFormat := "%.0f"

			if yAxisMap, ok := el.Axis["y"].(map[string]interface{}); ok {
				if show, ok := yAxisMap["show"].(bool); ok {
					showYAxis = show
				}
				yStep = getFloatVal(yAxisMap, "min_step", 0.0)
				yAxisW = getFloatVal(yAxisMap, "w", yAxisW)
				yAxisFg = getStringVal(yAxisMap, "fg", yAxisFg)

				if labelsMap, ok := yAxisMap["labels"].(map[string]interface{}); ok {
					if show, ok := labelsMap["show"].(bool); ok {
						showYLabels = show
					}
					if cVal, ok := labelsMap["count"].(int); ok {
						yLabelsCount = cVal
					}
					yLabelsFormat = getStringVal(labelsMap, "format", yLabelsFormat)

					if fVal, ok := labelsMap["font"]; ok {
						if face, ok := fVal.(string); ok && face != "" {
							yAxisFont = face
						} else if fMap, ok := fVal.(map[string]interface{}); ok {
							yAxisFont = getStringVal(fMap, "face", yAxisFont)
							yAxisFontFg = getStringVal(fMap, "fg", yAxisFontFg)
						}
					}

					if thsRaw, ok := labelsMap["thresholds"].([]interface{}); ok {
						for _, th := range thsRaw {
							if tmap, ok := th.(map[string]interface{}); ok {
								zstyle := make(map[string]string)
								if cond, ok := tmap["condition"].(string); ok {
									zstyle["condition"] = cond
								}
								if bg, ok := tmap["bg"].(string); ok {
									zstyle["bg"] = bg
								}
								if fg, ok := tmap["fg"].(string); ok {
									zstyle["fg"] = fg
								}
								zoneStyles = append(zoneStyles, zstyle)
							}
						}
					}
				}

				if notchesMap, ok := yAxisMap["notches"].(map[string]interface{}); ok {
					if show, ok := notchesMap["show"].(bool); ok {
						yNotchesVisible = show
					}
					yNotchesFg = getStringVal(notchesMap, "fg", yNotchesFg)
					yNotchesW = getFloatVal(notchesMap, "w", yNotchesW)
					yNotchesH = getFloatVal(notchesMap, "h", yNotchesH)
				}
			}

			showXAxis := true
			showXLabels := true
			xAxisW := 2.0
			xAxisFg := "black"
			xAxisFont := "tiny"
			xAxisFontFg := "black"
			notchesVisible := true
			notchesFg := "black"
			notchesW := 2.0
			notchesH := 6.0
			labelsCount := 5
			labelsFormat := "15:04"

			if xAxisMap, ok := el.Axis["x"].(map[string]interface{}); ok {
				if show, ok := xAxisMap["show"].(bool); ok {
					showXAxis = show
				}
				xAxisW = getFloatVal(xAxisMap, "h", xAxisW)
				xAxisFg = getStringVal(xAxisMap, "fg", xAxisFg)

				if labelsMap, ok := xAxisMap["labels"].(map[string]interface{}); ok {
					if show, ok := labelsMap["show"].(bool); ok {
						showXLabels = show
					}
					if cVal, ok := labelsMap["count"].(int); ok {
						labelsCount = cVal
					}
					labelsFormat = getStringVal(labelsMap, "format", labelsFormat)
					if fVal, ok := labelsMap["font"]; ok {
						if face, ok := fVal.(string); ok && face != "" {
							xAxisFont = face
						} else if fMap, ok := fVal.(map[string]interface{}); ok {
							xAxisFont = getStringVal(fMap, "face", xAxisFont)
							xAxisFontFg = getStringVal(fMap, "fg", xAxisFontFg)
						}
					}
				}
				if notchesMap, ok := xAxisMap["notches"].(map[string]interface{}); ok {
					if show, ok := notchesMap["show"].(bool); ok {
						notchesVisible = show
					}
					notchesFg = getStringVal(notchesMap, "fg", notchesFg)
					notchesW = getFloatVal(notchesMap, "w", notchesW)
					notchesH = getFloatVal(notchesMap, "h", notchesH)
				}
			}

			// Render X horizontal line
			if showXAxis {
				dc.SetLineWidth(xAxisW)
				dc.SetHexColor(rc.parseColor(xAxisFg))
				dc.DrawLine(el.Position.X, el.Position.Y+el.Size.H, el.Position.X+el.Size.W, el.Position.Y+el.Size.H)
				dc.Stroke()
			}

			// Render Y vertical line
			if showYAxis {
				dc.SetLineWidth(yAxisW)
				dc.SetHexColor(rc.parseColor(yAxisFg))
				dc.DrawLine(el.Position.X, el.Position.Y, el.Position.X, el.Position.Y+el.Size.H)
				dc.Stroke()
			}

			// Render Y scale labels
			if el.Axis != nil && el.Axis["y"] != nil {
				yFace := rc.getFontFace(yAxisFont)
				var yVals []float64
				if yStep > 0 {
					for val := 0.0; val <= maxVal; val += yStep {
						yVals = append(yVals, val)
					}
				} else if yLabelsCount > 1 {
					for i := 0; i < yLabelsCount; i++ {
						yVals = append(yVals, float64(i)*(maxVal/float64(yLabelsCount-1)))
					}
				} else {
					for val := 0.0; val <= maxVal; val += 50.0 {
						yVals = append(yVals, val)
					}
				}

				for _, val := range yVals {
					ly := el.Position.Y + el.Size.H - (val/maxVal)*el.Size.H

					bg := ""
					fg := yAxisFontFg
					// Check local conditions on coordinate ticks
					localTel := make(map[string]interface{})
					for k, v := range telemetry {
						localTel[k] = v
					}
					localTel[el.Source] = val
					localTel[cleanBase(el.Source)] = val

					for _, zs := range zoneStyles {
						if evalCondition(zs["condition"], localTel) {
							bg = zs["bg"]
							fg = zs["fg"]
							break
						}
					}

					if yNotchesVisible {
						dc.SetLineWidth(yNotchesW)
						dc.SetHexColor(rc.parseColor(yNotchesFg))
						dc.DrawLine(el.Position.X-yNotchesH, ly, el.Position.X, ly)
						dc.Stroke()
					}

					if showYLabels {
						textStr := fmt.Sprintf(yLabelsFormat, val)
						xMin, yMin, xMax, yMax := getStringBounds(yFace, textStr)
						w := xMax - xMin
						h := yMax - yMin

						pad := 4.0
						valX := el.Position.X - yNotchesH - 3 - xMax
						valY := ly - (yMin+yMax)/2

						if bg != "" && bg != "transparent" {
							dc.SetHexColor(rc.parseColor(bg))
							dc.DrawRoundedRectangle(el.Position.X-yNotchesH-3-w-pad, ly-h/2-pad, w+pad*2, h+pad*2, 2)
							dc.Fill()
						}

						dc.SetHexColor(rc.parseColor(fg))
						rc.loadFont(yAxisFont)
						dc.DrawString(textStr, math.Round(valX), math.Round(valY))
					}
				}
			}

			// Render X labels
			if el.Axis != nil && el.Axis["x"] != nil {
				labels := make([]string, labelsCount)
				duration := 24 * time.Hour
				if el.Duration != "" {
					if d, err := time.ParseDuration(el.Duration); err == nil {
						duration = d
					}
				}

				// Distribute ticks linearly across time range
				endTime := time.Now()
				startTime := endTime.Add(-duration)
				labelInterval := duration / time.Duration(labelsCount-1)

				for i := 0; i < labelsCount; i++ {
					t := startTime.Add(labelInterval * time.Duration(i))
					labels[i] = t.Local().Format(labelsFormat)
				}

				step := el.Size.W / float64(len(labels)-1)
				for i, lbl := range labels {
					tickX := el.Position.X + float64(i)*step
					if notchesVisible {
						dc.SetLineWidth(notchesW)
						dc.SetHexColor(rc.parseColor(notchesFg))
						dc.DrawLine(tickX, el.Position.Y+el.Size.H, tickX, el.Position.Y+el.Size.H+notchesH)
						dc.Stroke()
					}
					if showXLabels {
						rc.loadFont(xAxisFont)
						dc.SetHexColor(rc.parseColor(xAxisFontFg))
						// Draw anchored x label
						w, h := dc.MeasureString(lbl)
						lx := tickX - 0.5*w
						ly := el.Position.Y + el.Size.H + notchesH + 12 + (1.0-0.5)*h
						dc.DrawString(lbl, math.Round(lx), math.Round(ly))
					}
				}
			}
		}
	}

	return dc.Image(), nil
}

// getFloatVal helper to extract floats from axis configs
func getFloatVal(m map[string]interface{}, key string, def float64) float64 {
	if val, ok := m[key]; ok {
		if f, ok := val.(float64); ok {
			return f
		}
		if i, ok := val.(int); ok {
			return float64(i)
		}
	}
	return def
}

// getStringVal helper to extract string values from configs
func getStringVal(m map[string]interface{}, key string, def string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return def
}

// getThreshold resolves matching threshold configuration
func getThreshold(telemetry map[string]interface{}, thresholds []Threshold) Threshold {
	for _, t := range thresholds {
		if evalCondition(t.Condition, telemetry) {
			return t
		}
	}
	return Threshold{}
}

// evalCondition evaluates if condition matches current telemetry values
func evalCondition(cond string, telemetry map[string]interface{}) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true
	}

	parts := strings.Fields(cond)
	if len(parts) != 3 {
		return false
	}

	varName := parts[0]
	op := parts[1]
	rightStr := parts[2]

	var leftVal float64
	val, ok := telemetry[varName]
	if !ok {
		return false
	}

	switch v := val.(type) {
	case float64:
		leftVal = v
	case int:
		leftVal = float64(v)
	case int64:
		leftVal = float64(v)
	default:
		fmt.Sscanf(fmt.Sprintf("%v", val), "%f", &leftVal)
	}

	var rightVal float64
	if _, err := fmt.Sscanf(rightStr, "%f", &rightVal); err != nil {
		return false
	}

	switch op {
	case "==":
		return leftVal == rightVal
	case "!=":
		return leftVal != rightVal
	case ">=":
		return leftVal >= rightVal
	case "<=":
		return leftVal <= rightVal
	case ">":
		return leftVal > rightVal
	case "<":
		return leftVal < rightVal
	}

	return false
}

// PackEPDRaw maps image pixels to logical palette colors and packs them into raw binary buffers.
func PackEPDRaw(img image.Image, width, height int, mapping map[string][]int, palette map[string]string) []byte {
	parsedPalette := make(map[string]color.RGBA)
	for name, hex := range palette {
		c, err := parseHexColor(hex)
		if err == nil {
			parsedPalette[name] = c
		}
	}

	// Determine buffer sizes (1 bit per pixel)
	bufSize := (width * height) / 8
	if (width * height)%8 != 0 {
		bufSize++
	}

	// Determine number of buffers from the first mapping element
	numBuffers := 1
	for _, bits := range mapping {
		numBuffers = len(bits)
		break
	}

	buffers := make([][]byte, numBuffers)
	for i := 0; i < numBuffers; i++ {
		buffers[i] = make([]byte, bufSize)
	}

	// Pack pixels to buffers
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixelColor := img.At(x, y)
			r, g, b, _ := pixelColor.RGBA()
			pRGBA := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}

			// Map to closest palette color
			minDist := math.MaxFloat64
			closestColorName := ""
			for name, pColor := range parsedPalette {
				dist := colorDistance(pRGBA, pColor)
				if dist < minDist {
					minDist = dist
					closestColorName = name
				}
			}

			// Default if not matched
			if closestColorName == "" {
				closestColorName = "white"
			}

			bits := mapping[closestColorName]
			pixelIndex := y*width + x
			byteIndex := pixelIndex / 8
			bitOffset := 7 - (pixelIndex % 8) // MSB first

			for i := 0; i < len(bits); i++ {
				if i < numBuffers {
					if bits[i] != 0 {
						buffers[i][byteIndex] |= (1 << bitOffset)
					}
				}
			}
		}
	}

	// Concatenate EPD buffers sequentially
	var outputBytes []byte
	for i := 0; i < numBuffers; i++ {
		outputBytes = append(outputBytes, buffers[i]...)
	}
	return outputBytes
}

// EncodeImage wraps standard PNG, BMP, and EPD raw encoders.
func EncodeImage(img image.Image, format string, mapping map[string][]int, palette map[string]string) ([]byte, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("encoding PNG: %w", err)
		}
		return buf.Bytes(), nil

	case "bmp":
		var buf bytes.Buffer
		if err := bmp.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("encoding BMP: %w", err)
		}
		return buf.Bytes(), nil

	case "epd_raw":
		if len(mapping) == 0 {
			return nil, fmt.Errorf("cannot pack EPD bytes: palette mapping is empty")
		}
		return PackEPDRaw(img, width, height, mapping, palette), nil

	default:
		// Fallback to PNG
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("encoding default PNG: %w", err)
		}
		return buf.Bytes(), nil
	}
}

// RenderErrorImage renders a visual error message screen.
// Always works since it falls back to the embedded Tamzen font.
func RenderErrorImage(width, height int, errMsg string, format string, mapping map[string][]int, palette map[string]string) []byte {
	dc := gg.NewContext(width, height)
	dc.SetHexColor("#FFFFFF")
	dc.Clear()

	// Red border frame for warning
	dc.SetHexColor("#FF0000")
	dc.SetLineWidth(6)
	dc.DrawRectangle(10, 10, float64(width-20), float64(height-20))
	dc.Stroke()

	// Render text using embedded font face
	face, err := opentype.NewFace(embeddedFont, &opentype.FaceOptions{
		Size:    20.0,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err == nil {
		dc.SetFontFace(face)
	}

	dc.SetHexColor("#000000")
	dc.DrawStringWrapped("DASHBOARD ERROR:\n\n"+errMsg, float64(width/2), float64(height/2), 0.5, 0.5, float64(width-40), 1.5, gg.AlignCenter)

	img := dc.Image()
	res, errEncode := EncodeImage(img, format, mapping, palette)
	if errEncode != nil {
		// Extreme fallback: plain black/white raw bytes or PNG
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return buf.Bytes()
	}
	return res
}
