// Package tgbot implements the Telegram bot logic, command handlers, keyboards,
// and state storage.
// This file implements helper functions to generate, format, and render line charts (using go-analyze/charts)
// representing sensor history (AQI, PM2.5, PM10, Temperature, Humidity, Pressure).
package tgbot

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/aqinotifier/sensor"
	"github.com/go-analyze/charts"
)

func (b *Bot) getChartColor(key string) charts.Color {
	hex := b.C(key)
	if hex == "" {
		// Fallback to defaultColors if key not found in colorsMap
		if c, ok := defaultColors[key]; ok {
			return charts.ParseColor(c)
		}
		return charts.Color{R: 0, G: 0, B: 0, A: 255}
	}
	return charts.ParseColor(hex)
}

const (
	chartStrokeWidth      = 3.0
	chartSmoothingHistory = 0.1
	chartSmoothing24h     = 0.4

	chartPadLeft   = 3.5
	chartPadRight  = 6.0
	chartPadTop    = 4.0
	chartPadBottom = 5.5

	chartAxisDigitWeight = 0.9
	chartAxisSepWeight   = 0.3
	chartAxisBase        = 0.85

	chartXAxisHeightCoef = 2.0
	chartTitleHeightCoef = 3.0
	chartLabelFontCoef   = 0.8
	chartBarWidthCoef    = 0.01
	chartTextHalfLenCoef = 5.0
	chartLabelXOffsetL   = 0.6
	chartLabelXOffsetR   = 0.4

	chartHeadroomCoef    = 0.1
	chartTitleFontCoef   = 1.2
	chartDefaultMin      = 0.0
	chartDefaultMax      = 10.0
	chartLabelColorAlpha = 255

	chartThresholdPaddingCoef = 3.0
	chartDotLargeCoef         = 2.4
	chartDotSmallCoef         = 0.8
	chartDashWidthCoef        = 0.35

	chartAggregationWindow = 15 * time.Minute

	celsiusToFahrenheitSlope  = 1.8
	celsiusToFahrenheitOffset = 32.0

	magnusB = 17.27
	magnusC = 237.7

	chartMetaFontSizeCoef = 2.0 / 3.0
	chartMetaY1Coef       = 0.4
	chartMetaY2Coef       = 0.8
)

var (
	chartDashPattern = []float64{4, 4}
)

func chartFormatter(f float64, isAQI bool) string {
	if isAQI {
		return fmt.Sprintf("%d", int(math.Round(f)))
	}
	return fmt.Sprintf("%.1f", f)
}

func calcTextWidth(s string, fs float64) float64 {
	var w float64
	for _, r := range s {
		if r == '.' || r == ',' || r == ':' || r == ' ' || r == '(' || r == ')' || r == '-' {
			w += chartAxisSepWeight
		} else {
			w += chartAxisDigitWeight
		}
	}
	return fs * w
}

func calcYAxisWidth(fs float64, yMin, yMax float64, isAQI bool) int {
	s1 := chartFormatter(yMin, isAQI)
	s2 := chartFormatter(yMax, isAQI)
	s := s1
	if len(s2) > len(s1) {
		s = s2
	}
	return int(calcTextWidth(s, fs) + fs*chartAxisBase)
}

func generateCharts(b *Bot, chatID int64, hist []monitor.Measurement, chartWidth, chartHeight int, chartFontSize float64, smooth float64) ([][]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	var labels []string
	var pm10Values, pm25Values []float64
	var aqiValues []float64
	var tempValues, humValues, pressValues, dewPointValues []float64

	isF := b.store.GetUnitTemp(chatID) == "f"
	mcfg := b.GetUserSettings(chatID)
	for _, m := range hist {
		local := m.Timestamp.Local()
		label := local.Format(b.T(chatID, "formatChartLabel"))
		labels = append(labels, label)
		pm10Values = append(pm10Values, m.PM10)
		pm25Values = append(pm25Values, m.PM25)

		var aqi float64
		if mcfg.AQIStandard == "US" {
			aqi, _ = sensor.CalculateUS_AQI(m.PM25, m.PM10)
		} else {
			aqi, _ = sensor.CalculateEU_AQI(m.PM25, m.PM10)
		}
		aqiValues = append(aqiValues, aqi)

		if m.Temperature != 0 {
			t := b.convertTemp(m.Temperature, chatID)
			tempValues = append(tempValues, t)
			if m.Humidity != 0 {
				dp := CalcDewPoint(m.Temperature, m.Humidity)
				if isF {
					dp = dp*celsiusToFahrenheitSlope + celsiusToFahrenheitOffset
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

	deviceID := ""
	if len(hist) > 0 {
		deviceID = hist[0].DeviceID
	}

	var buffers [][]byte
	if len(aqiValues) > 0 {
		aqiTitle := b.T(chatID, "txtChartTitleAqi")
		aqiBuf, err := b.buildChart(chatID, deviceID, aqiTitle, mcfg.AQIStandard, labels, []string{"AQI"}, [][]float64{aqiValues}, true, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		if err == nil {
			buffers = append(buffers, aqiBuf)
		}
	}

	pmTitle := b.T(chatID, "txtChartPmTitle")
	pmBuf, err := b.buildChart(chatID, deviceID, pmTitle, b.T(chatID, "txtUnitPm"), labels,
		[]string{"PM2.5", "PM10"},
		[][]float64{pm25Values, pm10Values}, true, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
	if err == nil {
		buffers = append(buffers, pmBuf)
	}

	if len(tempValues) > 0 {
		buf, err := b.buildChart(chatID, deviceID, b.T(chatID, "txtChartTitleTemp"), b.unitTempLabel(chatID), labels,
			[]string{b.T(chatID, "msgTemp"), b.T(chatID, "msgDewPoint")},
			[][]float64{tempValues, dewPointValues}, false, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(humValues) > 0 {
		buf, err := b.buildChart(chatID, deviceID, b.T(chatID, "txtChartTitleHum"), "%", labels, []string{b.T(chatID, "msgHum")}, [][]float64{humValues}, false, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	if len(pressValues) > 0 {
		buf, err := b.buildChart(chatID, deviceID, b.T(chatID, "txtChartTitlePress"), b.unitPressLabel(chatID), labels, []string{b.T(chatID, "msgPress")}, [][]float64{pressValues}, false, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		if err == nil {
			buffers = append(buffers, buf)
		}
	}

	return buffers, nil
}

func generateSingleChart(b *Bot, chatID int64, hist []monitor.Measurement, chartType string, chartWidth, chartHeight int, chartFontSize float64, smooth float64) ([]byte, error) {
	if len(hist) == 0 {
		return nil, nil
	}

	window := chartAggregationWindow
	type bucket struct {
		pm10, pm25, temp, hum, press float64
		count                        int
	}
	buckets := make(map[int64]*bucket)
	var keys []int64
	for _, m := range hist {
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
	filteredHist := resampled

	var labels []string
	var pm10Values, pm25Values, aqiValues, tempValues, humValues, pressValues, dewPointValues []float64
	isF := b.store.GetUnitTemp(chatID) == "f"
	mcfg := b.GetUserSettings(chatID)

	for _, m := range filteredHist {
		local := m.Timestamp.Local()
		label := local.Format(b.T(chatID, "formatChartLabel"))
		labels = append(labels, label)
		switch chartType {
		case "pm":
			pm10Values = append(pm10Values, m.PM10)
			pm25Values = append(pm25Values, m.PM25)
		case "aqi":
			var aqi float64
			if mcfg.AQIStandard == "US" {
				aqi, _ = sensor.CalculateUS_AQI(m.PM25, m.PM10)
			} else {
				aqi, _ = sensor.CalculateEU_AQI(m.PM25, m.PM10)
			}
			aqiValues = append(aqiValues, aqi)
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

	deviceID := ""
	if len(hist) > 0 {
		deviceID = hist[0].DeviceID
	}

	pmTitle := b.T(chatID, "txtChartPmTitle")
	aqiTitle := b.T(chatID, "btnChartAqi")
	pmUnit := b.T(chatID, "txtUnitPm")
	switch chartType {
	case "pm":
		return b.buildChart(chatID, deviceID, pmTitle, pmUnit,
			labels, []string{"PM2.5", "PM10"},
			[][]float64{pm25Values, pm10Values}, true, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
	case "temp":
		if len(tempValues) > 0 {
			return b.buildChart(chatID, deviceID, b.T(chatID, "msgTemp"), b.unitTempLabel(chatID),
				labels, []string{b.T(chatID, "msgTemp"), b.T(chatID, "msgDewPoint")},
				[][]float64{tempValues, dewPointValues}, false, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		}
	case "hum":
		if len(humValues) > 0 {
			return b.buildChart(chatID, deviceID, b.T(chatID, "msgHum"), "%", labels, []string{b.T(chatID, "msgHum")}, [][]float64{humValues}, false, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		}
	case "press":
		if len(pressValues) > 0 {
			return b.buildChart(chatID, deviceID, b.T(chatID, "msgPress"), b.unitPressLabel(chatID), labels, []string{b.T(chatID, "msgPress")}, [][]float64{pressValues}, false, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		}
	case "aqi":
		if len(aqiValues) > 0 {
			return b.buildChart(chatID, deviceID, aqiTitle, mcfg.AQIStandard, labels, []string{"AQI"}, [][]float64{aqiValues}, true, chartWidth, chartHeight, chartFontSize, mcfg, smooth)
		}
	}

	return nil, nil
}

func (b *Bot) buildChart(chatID int64, deviceID string, title, yAxisName string, labels []string, seriesNames []string, data [][]float64, forceZero bool, chartWidth, chartHeight int, chartFontSize float64, mcfg *config.Monitor, smooth float64) ([]byte, error) {
	isPM := title == b.T(chatID, "txtChartPmTitle")
	isAQI := strings.Contains(title, "AQI")

	yMin, yMax := math.MaxFloat64, -math.MaxFloat64
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
		yMin, yMax = chartDefaultMin, chartDefaultMax
	} else if forceZero {
		yMin = 0
		yMax *= (1.0 + chartHeadroomCoef)
	} else {
		span := yMax - yMin
		if span == 0 {
			yMin -= 1
			yMax += 1
		} else {
			yMin -= span * chartHeadroomCoef
			yMax += span * chartHeadroomCoef
		}
	}

	pLeft := chartFontSize * chartPadLeft
	pRight := chartFontSize * chartPadRight
	pTop := chartFontSize * chartPadTop
	pBottom := chartFontSize * chartPadBottom

	opt := charts.NewLineChartOptionWithData(data)
	opt.Padding = charts.Box{
		Left:   int(pLeft),
		Right:  int(pRight),
		Top:    int(pTop),
		Bottom: int(pBottom),
	}
	titleText := fmt.Sprintf("%s (%s)", stripIcons(title), yAxisName)
	opt.Title = charts.TitleOption{
		Text:      titleText,
		FontStyle: charts.FontStyle{FontSize: chartFontSize * chartTitleFontCoef},
	}

	opt.XAxis = charts.XAxisOption{
		Show:                 charts.Ptr(true),
		BoundaryGap:          charts.Ptr(false),
		Labels:               labels,
		LabelCount:           8,
		LabelCountAdjustment: 2,
		LabelFontStyle: charts.FontStyle{
			FontSize:  chartFontSize * 0.8,
			FontColor: b.getChartColor("colorAxisLabel"),
		},
	}
	opt.YAxis = []charts.YAxisOption{
		{
			Min:            charts.Ptr(yMin),
			Max:            charts.Ptr(yMax),
			Show:           charts.Ptr(true),
			ValueFormatter: func(f float64) string { return chartFormatter(f, isAQI) },
			LabelFontStyle: charts.FontStyle{FontSize: chartFontSize},
		},
	}
	opt.Legend = charts.LegendOption{
		FontStyle: charts.FontStyle{FontSize: chartFontSize},
	}
	opt.LineStrokeWidth = chartStrokeWidth
	opt.StrokeSmoothingTension = smooth

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

	var theme = charts.GetDefaultTheme()
	if isPM {
		opt.Theme = theme.WithSeriesColors([]charts.Color{b.getChartColor("colorRed"), b.getChartColor("colorBlue")})
		opt.Theme = opt.Theme.WithBackgroundColor(charts.ColorTransparent)
		for i := 0; i < len(opt.SeriesList); i++ {
			opt.SeriesList[i].Name = seriesNames[i]
		}
	} else if isAQI {
		opt.Theme = theme.WithSeriesColors([]charts.Color{b.getChartColor("colorGray")})
		for i, name := range seriesNames {
			opt.SeriesList[i].Name = name
		}
	} else {
		colors := []charts.Color{b.getChartColor("colorBlue")}
		if strings.Contains(title, b.T(chatID, "msgTemp")) {
			colors = []charts.Color{b.getChartColor("colorRed"), b.getChartColor("colorBlue")}
		} else if strings.Contains(title, b.T(chatID, "msgPress")) {
			colors = []charts.Color{b.getChartColor("colorPurple")}
		}
		opt.Theme = theme.WithSeriesColors(colors)
		for i, name := range seriesNames {
			opt.SeriesList[i].Name = name
		}
	}

	if err := p.LineChart(opt); err != nil {
		return nil, err
	}

	cp := p.Child(charts.PainterPaddingOption(opt.Padding))
	yAxisWidth := calcYAxisWidth(chartFontSize, yMin, yMax, isAQI)
	xAxisHeight := int(chartFontSize * chartXAxisHeightCoef)
	titleHeight := int(chartFontSize * chartTitleHeightCoef)

	pureCp := cp.Child(charts.PainterPaddingOption(charts.Box{
		Left:   yAxisWidth,
		Bottom: xAxisHeight,
		Top:    titleHeight,
	}))

	gridW := float64(pureCp.Width())
	gridH := float64(pureCp.Height())

	metaFontSize := chartFontSize * chartMetaFontSizeCoef
	metaStyle := charts.FontStyle{
		FontSize:  metaFontSize,
		FontColor: b.getChartColor("colorAxisLabel"),
	}
	deviceStr := b.formatDeviceIDPlain(chatID, deviceID)
	timeStr := b.T(chatID, "msgChartTimestamp", map[string]interface{}{"date": time.Now(), "time": time.Now()})

	rightEdge := yAxisWidth + int(gridW)
	xDevice := rightEdge - int(calcTextWidth(deviceStr, metaFontSize))
	xTime := rightEdge - int(calcTextWidth(timeStr, metaFontSize))

	cp.Text(deviceStr, xDevice, int(float64(titleHeight)*chartMetaY1Coef), 0, metaStyle)
	cp.Text(timeStr, xTime, int(float64(titleHeight)*chartMetaY2Coef), 0, metaStyle)

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

	drawSeriesPoints := func(seriesData []float64, color charts.Color, yOffset float64) {
		// Dots on data points are now disabled globally.
		// Threshold crossing dots are handled by drawDots function.
	}

	drawDots := func(seriesData []float64, threshold float64, color charts.Color, yOffset float64) {
		if len(seriesData) < 2 {
			return
		}
		yT := yFunc(threshold) + yOffset
		for i := 0; i < len(seriesData)-1; i++ {
			v1 := seriesData[i]
			v2 := seriesData[i+1]
			if (v1 <= threshold && v2 >= threshold) || (v1 >= threshold && v2 <= threshold) {
				if v1 == v2 {
					continue
				}
				t := (threshold - v1) / (v2 - v1)
				x1 := float64(i) / float64(len(seriesData)-1) * gridW
				x2 := float64(i+1) / float64(len(seriesData)-1) * gridW
				x := x1 + t*(x2-x1)
				pureCp.Circle(chartStrokeWidth*chartDotLargeCoef, int(x), int(yT), color, color, 0)
				pureCp.Circle(chartStrokeWidth*chartDotSmallCoef, int(x), int(yT), charts.ColorWhite, charts.ColorWhite, 0)
			}
		}
	}

	if isPM {
		barWidth := float64(chartWidth) * chartBarWidthCoef
		textHalfLen := int(chartFontSize * chartLabelFontCoef * chartTextHalfLenCoef)
		styleLeft := charts.FontStyle{
			FontSize:  chartFontSize * chartLabelFontCoef,
			FontColor: b.getChartColor("colorRed"),
		}
		styleRight := charts.FontStyle{
			FontSize:  chartFontSize * chartLabelFontCoef,
			FontColor: b.getChartColor("colorBlue"),
		}
		pureCp.Text(b.T(chatID, "txtChartScalePm25"), int(barWidth+chartFontSize*chartLabelXOffsetL), int(gridH/2)-textHalfLen, math.Pi/2, styleLeft)
		pureCp.Text(b.T(chatID, "txtChartScalePm10"), int(gridW-barWidth-chartFontSize*chartLabelXOffsetR), int(gridH/2)+textHalfLen, -math.Pi/2, styleRight)

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

		drawBar(0, barWidth, 0, mcfg.PM25L1, b.getChartColor("colorGreenZone"))
		drawBar(0, barWidth, mcfg.PM25L1, mcfg.PM25L2, b.getChartColor("colorYellowZone"))
		drawBar(0, barWidth, mcfg.PM25L2, math.MaxFloat64, b.getChartColor("colorRedZone"))
		drawBar(gridW-barWidth, gridW, 0, mcfg.PM10L1, b.getChartColor("colorGreenZone"))
		drawBar(gridW-barWidth, gridW, mcfg.PM10L1, mcfg.PM10L2, b.getChartColor("colorYellowZone"))
		drawBar(gridW-barWidth, gridW, mcfg.PM10L2, math.MaxFloat64, b.getChartColor("colorRedZone"))

		dashWidth := chartStrokeWidth * chartDashWidthCoef
		dashPattern := chartDashPattern

		drawThreshold := func(val float64, color charts.Color, isLeft bool, yOffset float64) {
			if val < yMin || val > yMax {
				return
			}
			y := yFunc(val) + yOffset
			var tx1, tx2 float64
			if isLeft {
				tx1 = 0.0
				tx2 = gridW - barWidth*chartThresholdPaddingCoef
			} else {
				tx1 = barWidth * chartThresholdPaddingCoef
				tx2 = gridW
			}
			pureCp.DashedLineStroke([]charts.Point{{X: int(tx1), Y: int(y)}, {X: int(tx2), Y: int(y)}}, color, dashWidth, dashPattern)
		}

		drawThreshold(mcfg.PM25L1, b.getChartColor("colorRed"), true, 0)
		drawDots(data[0], mcfg.PM25L1, b.getChartColor("colorRed"), 0)
		drawThreshold(mcfg.PM25L2, b.getChartColor("colorRed"), true, 0)
		drawDots(data[0], mcfg.PM25L2, b.getChartColor("colorRed"), 0)

		var g10Offset float64
		if mcfg.PM10L1 == mcfg.PM25L1 || mcfg.PM10L1 == mcfg.PM25L2 {
			g10Offset = dashWidth * 2
		}
		drawThreshold(mcfg.PM10L1, b.getChartColor("colorBlue"), false, g10Offset)
		drawDots(data[1], mcfg.PM10L1, b.getChartColor("colorBlue"), g10Offset)

		var y10Offset float64
		if mcfg.PM10L2 == mcfg.PM25L1 || mcfg.PM10L2 == mcfg.PM25L2 {
			y10Offset = dashWidth * 2
		}
		drawThreshold(mcfg.PM10L2, b.getChartColor("colorBlue"), false, y10Offset)
		drawDots(data[1], mcfg.PM10L2, b.getChartColor("colorBlue"), y10Offset)

		drawSeriesPoints(data[0], b.getChartColor("colorRed"), 0)
		drawSeriesPoints(data[1], b.getChartColor("colorBlue"), g10Offset)
	}

	if isAQI {
		breakpoints := sensor.IndexPointsEU
		if mcfg.AQIStandard == "US" {
			breakpoints = sensor.IndexPointsUS
		}
		colors := []charts.Color{
			b.getChartColor("colorGreen"), b.getChartColor("colorYellow"), b.getChartColor("colorOrange"),
			b.getChartColor("colorDarkRed"), b.getChartColor("colorViolet"), b.getChartColor("colorMaroon"), b.getChartColor("colorGray"),
		}
		if mcfg.AQIStandard == "EU" {
			colors = []charts.Color{
				b.getChartColor("colorLightBlue"), b.getChartColor("colorGreen"), b.getChartColor("colorYellow"),
				b.getChartColor("colorOrange"), b.getChartColor("colorDarkRed"), b.getChartColor("colorMaroon"),
			}
		}
		for i := 0; i < len(breakpoints)-1; i++ {
			low := breakpoints[i]
			high := breakpoints[i+1]
			if i == len(breakpoints)-2 && mcfg.AQIStandard == "US" {
				high = sensor.IndexPointsUS[len(sensor.IndexPointsUS)-1]
			}
			c := colors[i]
			c.A = 51
			pyBottom := yFunc(math.Max(low, yMin))
			pyTop := yFunc(math.Min(high, yMax))
			if pyBottom > pyTop {
				pureCp.FillArea([]charts.Point{
					{X: 0, Y: int(pyBottom)},
					{X: int(gridW), Y: int(pyBottom)},
					{X: int(gridW), Y: int(pyTop)},
					{X: 0, Y: int(pyTop)},
					{X: 0, Y: int(pyBottom)},
				}, c)
			}
		}
		dashWidth := chartStrokeWidth * chartDashWidthCoef
		dashPattern := chartDashPattern
		for i := 1; i < len(breakpoints); i++ {
			val := breakpoints[i]
			if val < yMin || val > yMax {
				continue
			}
			y := yFunc(val)
			pureCp.DashedLineStroke([]charts.Point{{X: 0, Y: int(y)}, {X: int(gridW), Y: int(y)}}, colors[i-1], dashWidth, dashPattern)
			drawDots(data[0], val, b.getChartColor("colorGray"), 0)
		}
		drawSeriesPoints(data[0], b.getChartColor("colorGray"), 0)
	}

	if !isPM && !isAQI {
		colors := []charts.Color{b.getChartColor("colorBlue")}
		if strings.Contains(title, b.T(chatID, "msgTemp")) {
			colors = []charts.Color{b.getChartColor("colorRed"), b.getChartColor("colorBlue")}
		} else if strings.Contains(title, b.T(chatID, "msgPress")) {
			colors = []charts.Color{b.getChartColor("colorPurple")}
		}
		for i, c := range colors {
			if i < len(data) {
				drawSeriesPoints(data[i], c, 0)
			}
		}
	}

	return p.Bytes()
}

func CalcDewPoint(t, rh float64) float64 {
	if rh == 0 {
		return charts.GetNullValue()
	}
	gamma := math.Log(rh/100.0) + (magnusB*t)/(magnusC+t)
	return (magnusC * gamma) / (magnusB - gamma)
}

func stripIcons(s string) string {
	// Remove HTML tags
	reHTML := regexp.MustCompile(`<[^>]*>`)
	s = reHTML.ReplaceAllString(s, "")

	// Remove emojis but keep ASCII and Cyrillic
	var sb strings.Builder
	for _, r := range s {
		if (r >= 0x0020 && r <= 0x007E) || (r >= 0x0400 && r <= 0x04FF) || r == 0x00B0 {
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}
