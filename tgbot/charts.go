package tgbot

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/go-analyze/charts"
)

var (
	colorGreenZone  = charts.Color{R: 0, G: 255, B: 0, A: 85}   // 33% opacity
	colorYellowZone = charts.Color{R: 255, G: 255, B: 0, A: 85} // 33% opacity
	colorRedZone    = charts.Color{R: 255, G: 0, B: 0, A: 85}   // 33% opacity

	colorSeriesRed    = charts.ParseColor("#80090799") // Red
	colorSeriesBlue   = charts.ParseColor("#0c4c8084") // Blue
	colorSeriesPurple = charts.ParseColor("#6d197c8c") // Purple
)

// generateCharts produces a slice of PNG buffers containing PM, Temperature,
// Humidity, and Pressure charts based on the provided measurement history.
func generateCharts(b *Bot, chatID int64, hist []monitor.Measurement, pm10Green, pm25Green, pm10Yellow, pm25Yellow float64, chartWidth, chartHeight int, chartFontSize float64) ([][]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	var labels []string
	var pm10Values, pm25Values []float64
	var tempValues, humValues, pressValues, dewPointValues []float64

	isF := b.store.GetUnitTemp(chatID) == "f"

	for _, m := range hist {
		labels = append(labels, m.Timestamp.Local().Format("15:04"))
		pm10Values = append(pm10Values, m.PM10)
		pm25Values = append(pm25Values, m.PM25)

		if m.Temperature != 0 {
			t := b.convertTemp(m.Temperature, chatID)
			tempValues = append(tempValues, t)
			if m.Humidity != 0 {
				dp := CalcDewPoint(m.Temperature, m.Humidity)
				if isF {
					dp = dp*1.8 + 32
				}
				dewPointValues = append(dewPointValues, dp)
			} else {
				dewPointValues = append(dewPointValues, charts.GetNullValue())
			}
		} else {
			tempValues = append(tempValues, charts.GetNullValue())
			dewPointValues = append(dewPointValues, charts.GetNullValue())
		}

		if m.Humidity != 0 {
			humValues = append(humValues, m.Humidity)
		} else {
			humValues = append(humValues, charts.GetNullValue())
		}

		if m.Pressure != 0 {
			pressValues = append(pressValues, b.convertPress(m.Pressure, chatID))
		} else {
			pressValues = append(pressValues, charts.GetNullValue())
		}
	}

	const strokeWidth = 3.0
	const smoothing = 0.4

	formatter := func(f float64) string {
		return fmt.Sprintf("%.1f", f)
	}

	pmTitle := b.T(chatID, "chart_pm_title")

	buildChart := func(title, yAxisName string, seriesNames []string, data [][]float64, forceZero bool) ([]byte, error) {
		isPM := strings.Contains(strings.ToLower(title), "pm")
		theme := charts.GetDefaultTheme()

		// Calculate Y axis range strictly from data curves
		var yMin, yMax float64 = math.MaxFloat64, -math.MaxFloat64
		hasData := false
		for _, series := range data {
			for _, v := range series {
				if v != charts.GetNullValue() {
					if v > yMax {
						yMax = v
					}
					if v < yMin {
						yMin = v
					}
					hasData = true
				}
			}
		}

		if !hasData {
			yMin, yMax = 0, 10
		} else if forceZero {
			yMin = 0
			yMax *= 1.1 // Add 10% headroom
		} else {
			// Add 10% margin for other charts
			span := yMax - yMin
			if span == 0 {
				yMin -= 1
				yMax += 1
			} else {
				yMin -= span * 0.1
				yMax += span * 0.1
			}
		}

		// Balanced padding to center the grid and provide space for labels
		pLeft := chartFontSize * 6.0
		pRight := chartFontSize * 4.5
		pTop := chartFontSize * 3.8
		pBottom := chartFontSize * 3.5

		opt := charts.NewLineChartOptionWithData(data)
		opt.Padding = charts.Box{
			Left:   int(pLeft),
			Right:  int(pRight),
			Top:    int(pTop),
			Bottom: int(pBottom),
		}
		opt.Title = charts.TitleOption{
			Text:      fmt.Sprintf("%s (%s)", title, yAxisName),
			FontStyle: charts.FontStyle{FontSize: chartFontSize * 1.2},
		}
		opt.XAxis = charts.XAxisOption{
			Labels:         labels,
			LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
		}
		opt.YAxis = []charts.YAxisOption{
			{
				Min:            charts.Ptr(yMin),
				Max:            charts.Ptr(yMax),
				Show:           charts.Ptr(true),
				ValueFormatter: formatter,
				LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
			},
		}
		opt.Legend = charts.LegendOption{
			FontStyle: charts.FontStyle{FontSize: chartFontSize},
		}
		opt.LineStrokeWidth = strokeWidth
		opt.StrokeSmoothingTension = smoothing

		p := charts.NewPainter(charts.PainterOptions{
			Width:  chartWidth,
			Height: chartHeight,
		})
		p.FillArea([]charts.Point{
			{X: 0, Y: 0},
			{X: chartWidth, Y: 0},
			{X: chartWidth, Y: chartHeight},
			{X: 0, Y: chartHeight},
			{X: 0, Y: 0},
		}, charts.ColorWhite)

		if isPM {
			// Main chart styling
			opt.Theme = charts.GetDefaultTheme().WithSeriesColors([]charts.Color{colorSeriesBlue, colorSeriesRed})
			opt.Theme = opt.Theme.WithBackgroundColor(charts.ColorTransparent)
			for i := 0; i < len(opt.SeriesList); i++ {
				opt.SeriesList[i].Name = seriesNames[i]
			}
		} else {
			colors := []charts.Color{colorSeriesBlue}
			if title == b.T(chatID, "msg_temp") {
				colors = []charts.Color{colorSeriesRed, colorSeriesBlue}
			} else if title == b.T(chatID, "msg_press") {
				colors = []charts.Color{colorSeriesPurple}
			}
			opt.Theme = theme.WithSeriesColors(colors)
			for i, name := range seriesNames {
				opt.SeriesList[i].Name = name
			}
		}

		if err := p.LineChart(opt); err != nil {
			return nil, err
		}

		if isPM {
			// 1. Get the content painter (excludes outer Padding)
			cp := p.Child(charts.PainterPaddingOption(opt.Padding))

			// 2. Define "internal" offsets for axes and titles to reach the "pure" grid area
			yAxisWidth := int(chartFontSize * 4.0)
			xAxisHeight := int(chartFontSize * 2.0)
			// Increased top offset to provide more space for the title
			titleHeight := int(chartFontSize * 3.5)

			// 3. Create a child painter for the ACTUAL grid area (the "pure" area)
			pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
				Left:   yAxisWidth,
				Bottom: xAxisHeight,
				Top:    titleHeight,
			}))

			gridW := float64(pureCp.Width())
			gridH := float64(pureCp.Height())

			// 4. Vertical axis labels (rotated and centered)
			styleLeft := charts.FontStyle{
				FontSize:  chartFontSize * 0.8,
				FontColor: colorSeriesBlue,
			}
			styleRight := charts.FontStyle{
				FontSize:  chartFontSize * 0.8,
				FontColor: colorSeriesRed,
			}

			barWidth := float64(chartWidth) * 0.01
			// Center vertically: subtract approx half-length of "Шкала PM10"
			textHalfLen := int(chartFontSize * 0.8 * 5.0)

			// PM10 label: inside the grid, just to the right of the PM10 bar
			pureCp.Text(b.T(chatID, "chart_scale_pm10"), int(barWidth+chartFontSize*0.6), int(gridH/2)-textHalfLen, math.Pi/2, styleLeft)

			// PM2.5 label: inside the grid, just to the right of its previous position (closer to bar)
			// -Pi/2 rotation goes UP, so we start at mid+half
			pureCp.Text(b.T(chatID, "chart_scale_pm25"), int(gridW-barWidth-chartFontSize*0.4), int(gridH/2)+textHalfLen, -math.Pi/2, styleRight)

			// Vertical range: exactly from 0 to gridH in pure coordinates
			yAxisBottom := gridH
			yAxisTop := 0.0
			plotHeight := yAxisBottom - yAxisTop

			yFunc := func(val float64) float64 {
				y := yAxisBottom - (val-yMin)/(yMax-yMin)*plotHeight
				if y < yAxisTop {
					y = yAxisTop
				}
				if y > yAxisBottom {
					y = yAxisBottom
				}
				return y
			}

			drawBar := func(x1, x2 float64, low, high float64, color charts.Color) {
				start := math.Max(low, yMin)
				end := math.Min(high, yMax)
				if start < end {
					pyBottom := yFunc(start)
					pyTop := yFunc(end)
					pureCp.FillArea([]charts.Point{
						{X: int(x1), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyTop)},
						{X: int(x1), Y: int(pyTop)},
						{X: int(x1), Y: int(pyBottom)},
					}, color)
				}
			}

			// PM10 bar on the left: exactly at the start of the grid (0 in pure coords)
			drawBar(0, barWidth, 0, pm10Green, colorGreenZone)
			drawBar(0, barWidth, pm10Green, pm10Yellow, colorYellowZone)
			drawBar(0, barWidth, pm10Yellow, math.MaxFloat64, colorRedZone)

			// PM2.5 bar on the right: exactly at the end of the grid (gridW in pure coords)
			drawBar(gridW-barWidth, gridW, 0, pm25Green, colorGreenZone)
			drawBar(gridW-barWidth, gridW, pm25Green, pm25Yellow, colorYellowZone)
			drawBar(gridW-barWidth, gridW, pm25Yellow, math.MaxFloat64, colorRedZone)

			dashWidth := strokeWidth * 0.35
			dashPattern := []float64{4, 4}

			drawThreshold := func(val float64, color charts.Color, isPM10 bool) {
				if val < yMin || val > yMax {
					return
				}
				y := yFunc(val)
				var tx1, tx2 float64
				if isPM10 {
					tx1 = 0.0
					tx2 = gridW - barWidth*3.0
				} else {
					tx1 = barWidth * 3.0
					tx2 = gridW
				}
				pureCp.DashedLineStroke([]charts.Point{{X: int(tx1), Y: int(y)}, {X: int(tx2), Y: int(y)}}, color, dashWidth, dashPattern)
			}

			// 5. Draw intersection dots where data crosses thresholds
			drawDots := func(seriesData []float64, threshold float64, color charts.Color) {
				if len(seriesData) < 2 {
					return
				}
				yT := yFunc(threshold)
				for i := 0; i < len(seriesData)-1; i++ {
					v1 := seriesData[i]
					v2 := seriesData[i+1]
					// Check crossing
					if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
						if v1 == v2 {
							continue
						}
						t := (threshold - v1) / (v2 - v1)
						x1 := float64(i) / float64(len(seriesData)-1) * gridW
						x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
						x := x1 + t*(x2-x1)
						// Draw inner white dot inside a larger colored dot
						pureCp.Circle(strokeWidth*2.4, int(x), int(yT), color, color, 0)
						pureCp.Circle(strokeWidth*0.8, int(x), int(yT), charts.ColorWhite, charts.ColorWhite, 0)
					}
				}
			}

			// Draw PM10 thresholds and dots
			drawThreshold(pm10Green, colorSeriesBlue, true)
			drawDots(data[0], pm10Green, colorSeriesBlue)
			drawThreshold(pm10Yellow, colorSeriesBlue, true)
			drawDots(data[0], pm10Yellow, colorSeriesBlue)

			// Draw PM2.5 thresholds and dots (with small offset if identical to PM10)
			g2 := pm25Green
			if g2 == pm10Green {
				g2 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(g2, colorSeriesRed, false)
			drawDots(data[1], g2, colorSeriesRed)

			y2 := pm25Yellow
			if y2 == pm10Yellow {
				y2 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(y2, colorSeriesRed, false)
			drawDots(data[1], y2, colorSeriesRed)
		}

		return p.Bytes()
	}

	var buffers [][]byte

	pmBuf, err := buildChart(pmTitle, b.T(chatID, "chart_unit_pm"),
		[]string{"PM10", "PM2.5"},
		[][]float64{pm10Values, pm25Values}, true)
	if err != nil {
		return nil, err
	}
	buffers = append(buffers, pmBuf)

	if len(tempValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_temp"), b.unitTempLabel(chatID),
			[]string{b.T(chatID, "msg_temp"), b.T(chatID, "msg_dew_point")},
			[][]float64{tempValues, dewPointValues}, false)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(humValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_hum"), "%", []string{b.T(chatID, "msg_hum")}, [][]float64{humValues}, false)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(pressValues) > 0 {
		buf, err := buildChart(b.T(chatID, "msg_press"), b.unitPressLabel(chatID), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues}, false)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	return buffers, nil
}

// generateSingleChart produces a single PNG buffer containing the requested chart type
// for the last 24 hours.
func generateSingleChart(b *Bot, chatID int64, hist []monitor.Measurement, chartType string, pm10Green, pm25Green, pm10Yellow, pm25Yellow float64, chartWidth, chartHeight int, chartFontSize float64) ([]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	filteredHist := hist

	// Resample to 15-minute intervals for better chart readability and stable labels
	window := 15 * time.Minute
	type bucket struct {
		pm10, pm25, temp, hum, press float64
		count                        int
	}
	buckets := make(map[int64]*bucket)
	var keys []int64
	for _, m := range filteredHist {
		k := m.Timestamp.Truncate(window).Unix()
		if _, ok := buckets[k]; !ok {
			keys = append(keys, k)
			buckets[k] = &bucket{}
		}
		b := buckets[k]
		b.pm10 += m.PM10
		b.pm25 += m.PM25
		b.temp += m.Temperature
		b.hum += m.Humidity
		b.press += m.Pressure
		b.count++
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	var resampled []monitor.Measurement
	for _, k := range keys {
		b := buckets[k]
		resampled = append(resampled, monitor.Measurement{
			Timestamp:   time.Unix(k, 0),
			PM10:        b.pm10 / float64(b.count),
			PM25:        b.pm25 / float64(b.count),
			Temperature: b.temp / float64(b.count),
			Humidity:    b.hum / float64(b.count),
			Pressure:    b.press / float64(b.count),
		})
	}
	filteredHist = resampled

	var labels []string
	var pm10Values, pm25Values []float64
	var tempValues, humValues, pressValues, dewPointValues []float64

	lastHour := -1
	isF := b.store.GetUnitTemp(chatID) == "f"

	for _, m := range filteredHist {
		local := m.Timestamp.Local()
		label := ""
		if local.Hour() != lastHour {
			label = local.Format("15:00")
			lastHour = local.Hour()
		}
		labels = append(labels, label)

		switch chartType {
		case "pm":
			pm10Values = append(pm10Values, m.PM10)
			pm25Values = append(pm25Values, m.PM25)
		case "temp":
			if m.Temperature != 0 {
				tempValues = append(tempValues, b.convertTemp(m.Temperature, chatID))
				if m.Humidity != 0 {
					dp := CalcDewPoint(m.Temperature, m.Humidity)
					if isF {
						dp = dp*1.8 + 32
					}
					dewPointValues = append(dewPointValues, dp)
				} else {
					dewPointValues = append(dewPointValues, charts.GetNullValue())
				}
			} else {
				tempValues = append(tempValues, charts.GetNullValue())
				dewPointValues = append(dewPointValues, charts.GetNullValue())
			}
		case "hum":
			if m.Humidity != 0 {
				humValues = append(humValues, m.Humidity)
			} else {
				humValues = append(humValues, charts.GetNullValue())
			}
		case "press":
			if m.Pressure != 0 {
				pressValues = append(pressValues, b.convertPress(m.Pressure, chatID))
			} else {
				pressValues = append(pressValues, charts.GetNullValue())
			}
		}
	}

	const strokeWidth = 3.0
	const smoothing = 0.4

	formatter := func(f float64) string {
		return fmt.Sprintf("%.1f", f)
	}

	pmTitle := b.T(chatID, "chart_pm_title")

	buildChart := func(title, yAxisName string, seriesNames []string, data [][]float64, forceZero bool) ([]byte, error) {
		isPM := strings.Contains(strings.ToLower(title), "pm")
		// Calculate Y axis range strictly from data curves
		var yMin, yMax float64 = math.MaxFloat64, -math.MaxFloat64
		hasData := false
		for _, series := range data {
			for _, v := range series {
				if v != charts.GetNullValue() {
					if v > yMax {
						yMax = v
					}
					if v < yMin {
						yMin = v
					}
					hasData = true
				}
			}
		}

		if !hasData {
			yMin, yMax = 0, 10
		} else if forceZero {
			yMin = 0
			yMax *= 1.1 // Add 10% headroom
		} else {
			// Add 10% margin for other charts
			span := yMax - yMin
			if span == 0 {
				yMin -= 1
				yMax += 1
			} else {
				yMin -= span * 0.1
				yMax += span * 0.1
			}
		}

		// Balanced padding to center the grid and provide space for labels
		pLeft := chartFontSize * 6.0
		pRight := chartFontSize * 4.5
		pTop := chartFontSize * 4.0
		pBottom := chartFontSize * 3.5

		opt := charts.NewLineChartOptionWithData(data)
		opt.Padding = charts.Box{
			Left:   int(pLeft),
			Right:  int(pRight),
			Top:    int(pTop),
			Bottom: int(pBottom),
		}
		opt.Title = charts.TitleOption{
			Text:      fmt.Sprintf("%s (%s)", title, yAxisName),
			FontStyle: charts.FontStyle{FontSize: chartFontSize * 1.2},
		}
		opt.XAxis = charts.XAxisOption{
			Labels:         labels,
			LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
		}
		opt.YAxis = []charts.YAxisOption{
			{
				Min:            charts.Ptr(yMin),
				Max:            charts.Ptr(yMax),
				Show:           charts.Ptr(true),
				ValueFormatter: formatter,
				LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
			},
		}
		opt.Legend = charts.LegendOption{
			FontStyle: charts.FontStyle{FontSize: chartFontSize},
		}
		opt.LineStrokeWidth = strokeWidth
		opt.StrokeSmoothingTension = smoothing

		p := charts.NewPainter(charts.PainterOptions{
			Width:  chartWidth,
			Height: chartHeight,
		})
		p.FillArea([]charts.Point{
			{X: 0, Y: 0},
			{X: chartWidth, Y: 0},
			{X: chartWidth, Y: chartHeight},
			{X: 0, Y: chartHeight},
			{X: 0, Y: 0},
		}, charts.ColorWhite)

		if isPM {
			// Main chart styling
			opt.Theme = charts.GetDefaultTheme().WithSeriesColors([]charts.Color{colorSeriesBlue, colorSeriesRed})
			opt.Theme = opt.Theme.WithBackgroundColor(charts.ColorTransparent)
			for i := 0; i < len(opt.SeriesList); i++ {
				opt.SeriesList[i].Name = seriesNames[i]
			}
		} else {
			colors := []charts.Color{colorSeriesBlue}
			if title == b.T(chatID, "msg_temp") {
				colors = []charts.Color{colorSeriesRed, colorSeriesBlue}
			} else if title == b.T(chatID, "msg_press") {
				colors = []charts.Color{colorSeriesPurple}
			}
			opt.Theme = charts.GetDefaultTheme().WithSeriesColors(colors)

			for i, name := range seriesNames {
				opt.SeriesList[i].Name = name
			}
		}

		if err := p.LineChart(opt); err != nil {
			return nil, err
		}

		if isPM {
			// 1. Get the content painter (excludes outer Padding)
			cp := p.Child(charts.PainterPaddingOption(opt.Padding))

			// 2. Define "internal" offsets for axes and titles to reach the "pure" grid area
			yAxisWidth := int(chartFontSize * 4.0)
			xAxisHeight := int(chartFontSize * 2.0)
			// Increased top offset to provide more space for the title
			titleHeight := int(chartFontSize * 3.5)

			// 3. Create a child painter for the ACTUAL grid area (the "pure" area)
			pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
				Left:   yAxisWidth,
				Bottom: xAxisHeight,
				Top:    titleHeight,
			}))

			gridW := float64(pureCp.Width())
			gridH := float64(pureCp.Height())

			// 4. Vertical axis labels (rotated and centered)
			styleLeft := charts.FontStyle{
				FontSize:  chartFontSize * 0.8,
				FontColor: colorSeriesBlue,
			}
			styleRight := charts.FontStyle{
				FontSize:  chartFontSize * 0.8,
				FontColor: colorSeriesRed,
			}

			barWidth := float64(chartWidth) * 0.01
			// Center vertically: subtract approx half-length of "Шкала PM10"
			textHalfLen := int(chartFontSize * 0.8 * 5.0)

			// PM10 label: inside the grid, just to the right of the PM10 bar
			pureCp.Text(b.T(chatID, "chart_scale_pm10"), int(barWidth+chartFontSize*0.6), int(gridH/2)-textHalfLen, math.Pi/2, styleLeft)

			// PM2.5 label: inside the grid, just to the right of its previous position (closer to bar)
			// -Pi/2 rotation goes UP, so we start at mid+half
			pureCp.Text(b.T(chatID, "chart_scale_pm25"), int(gridW-barWidth-chartFontSize*0.4), int(gridH/2)+textHalfLen, -math.Pi/2, styleRight)

			// Vertical range: exactly from 0 to gridH in pure coordinates
			yAxisBottom := gridH
			yAxisTop := 0.0
			plotHeight := yAxisBottom - yAxisTop

			yFunc := func(val float64) float64 {
				y := yAxisBottom - (val-yMin)/(yMax-yMin)*plotHeight
				if y < yAxisTop {
					y = yAxisTop
				}
				if y > yAxisBottom {
					y = yAxisBottom
				}
				return y
			}

			drawBar := func(x1, x2 float64, low, high float64, color charts.Color) {
				start := math.Max(low, yMin)
				end := math.Min(high, yMax)
				if start < end {
					pyBottom := yFunc(start)
					pyTop := yFunc(end)
					pureCp.FillArea([]charts.Point{
						{X: int(x1), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyBottom)},
						{X: int(x2), Y: int(pyTop)},
						{X: int(x1), Y: int(pyTop)},
						{X: int(x1), Y: int(pyBottom)},
					}, color)
				}
			}

			// PM10 bar on the left: exactly at the start of the grid (0 in pure coords)
			drawBar(0, barWidth, 0, pm10Green, colorGreenZone)
			drawBar(0, barWidth, pm10Green, pm10Yellow, colorYellowZone)
			drawBar(0, barWidth, pm10Yellow, math.MaxFloat64, colorRedZone)

			// PM2.5 bar on the right: exactly at the end of the grid (gridW in pure coords)
			drawBar(gridW-barWidth, gridW, 0, pm25Green, colorGreenZone)
			drawBar(gridW-barWidth, gridW, pm25Green, pm25Yellow, colorYellowZone)
			drawBar(gridW-barWidth, gridW, pm25Yellow, math.MaxFloat64, colorRedZone)

			dashWidth := strokeWidth * 0.35
			dashPattern := []float64{4, 4}

			drawThreshold := func(val float64, color charts.Color, isPM10 bool) {
				if val < yMin || val > yMax {
					return
				}
				y := yFunc(val)
				var tx1, tx2 float64
				if isPM10 {
					tx1 = 0.0
					tx2 = gridW - barWidth*3.0
				} else {
					tx1 = barWidth * 3.0
					tx2 = gridW
				}
				pureCp.DashedLineStroke([]charts.Point{{X: int(tx1), Y: int(y)}, {X: int(tx2), Y: int(y)}}, color, dashWidth, dashPattern)
			}

			// 5. Draw intersection dots where data crosses thresholds
			drawDots := func(seriesData []float64, threshold float64, color charts.Color) {
				if len(seriesData) < 2 {
					return
				}
				yT := yFunc(threshold)
				for i := 0; i < len(seriesData)-1; i++ {
					v1 := seriesData[i]
					v2 := seriesData[i+1]
					// Check crossing
					if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
						if v1 == v2 {
							continue
						}
						t := (threshold - v1) / (v2 - v1)
						x1 := float64(i) / float64(len(seriesData)-1) * gridW
						x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
						x := x1 + t*(x2-x1)
						// Draw inner white dot inside a larger colored dot
						pureCp.Circle(strokeWidth*2.4, int(x), int(yT), color, color, 0)
						pureCp.Circle(strokeWidth*0.8, int(x), int(yT), charts.ColorWhite, charts.ColorWhite, 0)
					}
				}
			}

			// Draw PM10 thresholds and dots
			drawThreshold(pm10Green, colorSeriesBlue, true)
			drawDots(data[0], pm10Green, colorSeriesBlue)
			drawThreshold(pm10Yellow, colorSeriesBlue, true)
			drawDots(data[0], pm10Yellow, colorSeriesBlue)

			// Draw PM2.5 thresholds and dots (with small offset if identical to PM10)
			g2 := pm25Green
			if g2 == pm10Green {
				g2 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(g2, colorSeriesRed, false)
			drawDots(data[1], g2, colorSeriesRed)

			y2 := pm25Yellow
			if y2 == pm10Yellow {
				y2 -= (yMax - yMin) / plotHeight
			}
			drawThreshold(y2, colorSeriesRed, false)
			drawDots(data[1], y2, colorSeriesRed)
		}

		return p.Bytes()
	}

	switch chartType {
	case "pm":
		return buildChart(pmTitle, b.T(chatID, "chart_unit_pm"),
			[]string{"PM10", "PM2.5"},
			[][]float64{pm10Values, pm25Values}, true)
	case "temp":
		if len(tempValues) > 0 {
			return buildChart(b.T(chatID, "msg_temp"), b.unitTempLabel(chatID),
				[]string{b.T(chatID, "msg_temp"), b.T(chatID, "msg_dew_point")},
				[][]float64{tempValues, dewPointValues}, false)
		}
	case "hum":
		if len(humValues) > 0 {
			return buildChart(b.T(chatID, "msg_hum"), "%", []string{b.T(chatID, "msg_hum")}, [][]float64{humValues}, false)
		}
	case "press":
		if len(pressValues) > 0 {
			return buildChart(b.T(chatID, "msg_press"), b.unitPressLabel(chatID), []string{b.T(chatID, "msg_press")}, [][]float64{pressValues}, false)
		}
	}

	return nil, nil
}

func CalcDewPoint(t, rh float64) float64 {
	if rh == 0 {
		return charts.GetNullValue()
	}
	// Magnus formula constants
	const b = 17.27
	const c = 237.7
	gamma := math.Log(rh/100.0) + (b*t)/(c+t)
	return (c * gamma) / (b - gamma)
}
