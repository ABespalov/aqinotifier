// Package dashboard handles loading layouts, managing visual endpoints,
// preparing telemetry and rendering dashboard screens to EPD, PNG, or BMP.
package dashboard

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ABespalov/aqinotifier/config"
	"github.com/ABespalov/aqinotifier/monitor"
	"github.com/ABespalov/csirender"
	"github.com/rs/zerolog/log"
)

// RegisterHandlers binds visual dashboard routing paths specified in the main configuration
// to the HTTP server multiplexer.
func RegisterHandlers(mux *http.ServeMux, appCfg *config.Config, ms *monitor.MonitorService) {
	if !appCfg.Dashboards.Enabled {
		return
	}

	for _, ep := range appCfg.Dashboards.Endpoints {
		path := ep.Path
		file := ep.File

		log.Info().Str("path", path).Str("file", file).Msg("dashboard: registering route")

		// Create a separate handler instance for each route
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			handleDashboardRequest(w, r, file, appCfg, ms)
		})
	}

	log.Info().Msg("dashboard: listener started")
}

// findDefaultDevice attempts to resolve a fallback device ID if the client did not supply one.
func findDefaultDevice(appCfg *config.Config) string {
	// Try finding the first mapped device name
	for id := range appCfg.Monitor.DeviceNames {
		if id != "" {
			return id
		}
	}
	// Try finding the first correction formula device prefix
	for id := range appCfg.Monitor.Corrections {
		if id != "" {
			return id
		}
	}
	return ""
}

// isAllowed validates if the client IP is allowed by the dashboard Access Control List (ACL).
func isAllowed(remoteAddr string, allowedCIDRs []string) bool {
	if len(allowedCIDRs) == 0 {
		return true // Allow all if allowed CIDR list is empty
	}

	// Split host and port from RemoteAddr
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	clientIP := net.ParseIP(host)
	if clientIP == nil {
		return false
	}

	for _, pattern := range allowedCIDRs {
		pattern = strings.TrimSpace(pattern)
		// Check CIDR format
		if _, ipNet, err := net.ParseCIDR(pattern); err == nil {
			if ipNet.Contains(clientIP) {
				return true
			}
		} else {
			// Check single IP format
			if ip := net.ParseIP(pattern); ip != nil {
				if ip.Equal(clientIP) {
					return true
				}
			}
		}
	}
	return false
}

// handleDashboardRequest handles incoming GET queries for visual dashboards.
// It loads layout configs dynamically, checks ACL, resolves telemetry/history,
// renders layouts, and handles panics/errors visually.
func handleDashboardRequest(w http.ResponseWriter, r *http.Request, layoutPath string, appCfg *config.Config, ms *monitor.MonitorService) {
	// Format settings (config defaults) used in case of panic/error page rendering
	errFormat := "png"
	var errMapping map[string][]int
	var errPalette map[string]string
	var errMarginX, errMarginY int
	var errFontSize int
	errWidth, errHeight := 800, 480 // fallback dimensions

	// Setup panic recovery to display error message directly on the e-ink display
	defer func() {
		if rec := recover(); rec != nil {
			errStr := fmt.Sprintf("Panic recovered: %v", rec)
			log.Error().Str("panic", errStr).Msg("dashboard: panic recovered during rendering")
			w.Header().Set("Content-Type", "image/png")
			errBytes := csirender.RenderErrorImage(errWidth, errHeight, errStr, errFormat, errMapping, errPalette, errFontSize, errMarginX, errMarginY)
			_, _ = w.Write(errBytes)
		}
	}()

	// 1. Dynamic Live Parsing of dashboard layout config
	layoutCfg, err := ParseConfig(layoutPath)
	if err != nil {
		log.Error().Err(err).Str("file", layoutPath).Msg("dashboard: failed to parse layout config")
		w.Header().Set("Content-Type", "image/png")
		errBytes := csirender.RenderErrorImage(errWidth, errHeight, "Failed to load layout: "+err.Error(), errFormat, errMapping, errPalette, errFontSize, errMarginX, errMarginY)
		_, _ = w.Write(errBytes)
		return
	}

	// Update error fallbacks with parsed config values
	errWidth = layoutCfg.Screen.Width
	errHeight = layoutCfg.Screen.Height
	errFormat = layoutCfg.Output.Format
	errMapping = layoutCfg.Output.Mapping
	errPalette = layoutCfg.Screen.Palette
	errMarginX = layoutCfg.Error.Margin.X
	errMarginY = layoutCfg.Error.Margin.Y
	errFontSize = layoutCfg.Error.Size

	// 2. Access Control List check
	if !isAllowed(r.RemoteAddr, layoutCfg.Allowed) {
		log.Warn().Str("remote", r.RemoteAddr).Str("file", layoutPath).Msg("dashboard: forbidden IP address")
		http.Error(w, "Forbidden: IP not allowed by ACL", http.StatusForbidden)
		return
	}

	// 3. Resolve Target Device ID
	deviceID := r.URL.Query().Get("device")
	if deviceID == "" {
		deviceID = r.URL.Query().Get("device_id")
	}
	if deviceID == "" {
		deviceID = findDefaultDevice(appCfg)
	}

	if deviceID == "" {
		log.Error().Msg("dashboard: no devices configured or requested")
		w.Header().Set("Content-Type", getContentType(errFormat))
		errBytes := csirender.RenderErrorImage(errWidth, errHeight, "No devices registered. Check device config or query params.", errFormat, errMapping, errPalette, errFontSize, errMarginX, errMarginY)
		_, _ = w.Write(errBytes)
		return
	}

	// 4. Fetch telemetry measurements
	m := ms.LastMeasurement(deviceID)
	if m == nil {
		log.Error().Str("device", deviceID).Msg("dashboard: no measurement telemetry found")
		w.Header().Set("Content-Type", getContentType(errFormat))
		errBytes := csirender.RenderErrorImage(errWidth, errHeight, "No telemetry found for device: "+deviceID, errFormat, errMapping, errPalette, errFontSize, errMarginX, errMarginY)
		_, _ = w.Write(errBytes)
		return
	}

	// 5. Load localization dictionaries and build parameters map
	lang := layoutCfg.Lang
	if lang == "" {
		lang = "en"
	}
	dict := LoadLanguageDict(lang)
	telemetryMap := BuildTelemetryMap(m, appCfg, dict)

	// 6. Load database history for chart components
	// Look up chart duration to pull right timeline depth
	maxDuration := 24 * time.Hour
	for _, wrap := range layoutCfg.Layout {
		if chartEl, ok := wrap.Element.(*csirender.ChartElement); ok {
			if chartEl.Duration != "" {
				if d, err := time.ParseDuration(chartEl.Duration); err == nil {
					if d > maxDuration {
						maxDuration = d
					}
				}
			}
		}
	}
	history := ms.GetHistoryByDuration(deviceID, maxDuration)
	charts := make(map[string][]float64)

	// Pre-sample data for each chart
	for _, wrap := range layoutCfg.Layout {
		if chartEl, ok := wrap.Element.(*csirender.ChartElement); ok {
			d := 24 * time.Hour
			if chartEl.Duration != "" {
				if p, err := time.ParseDuration(chartEl.Duration); err == nil {
					d = p
				}
			}
			points := chartEl.Points
			if points == 0 {
				points = 48
			}
			charts[chartEl.Source] = ResampleHistory(history, d, points, chartEl.Source, "")
		}
	}

	renderData := csirender.RenderData{
		Values: telemetryMap,
		Charts: charts,
	}

	engine := csirender.New()
	engine.Resolver = ResolveThresholdValue
	engine.Enricher = EnrichChartTelemetry

	// 7. Render layout elements to image
	img, err := engine.Render(&layoutCfg.LayoutConfig, renderData)
	if err != nil {
		log.Error().Err(err).Msg("dashboard: rendering failed")
		w.Header().Set("Content-Type", getContentType(errFormat))
		errBytes := csirender.RenderErrorImage(errWidth, errHeight, "Render failed: "+err.Error(), errFormat, errMapping, errPalette, errFontSize, errMarginX, errMarginY)
		_, _ = w.Write(errBytes)
		return
	}

	// 8. Determine target output format
	format := r.URL.Query().Get("format")
	if format == "" {
		format = layoutCfg.Output.Format
	}
	if format == "" {
		format = "png"
	}

	outputBytes, err := csirender.EncodeImage(img, format, layoutCfg.Output.Mapping, layoutCfg.Screen.Palette)
	if err != nil {
		log.Error().Err(err).Msg("dashboard: serialization failed")
		w.Header().Set("Content-Type", getContentType(errFormat))
		errBytes := csirender.RenderErrorImage(errWidth, errHeight, "Encode failed: "+err.Error(), errFormat, errMapping, errPalette, errFontSize, errMarginX, errMarginY)
		_, _ = w.Write(errBytes)
		return
	}

	w.Header().Set("Content-Type", getContentType(format))
	_, _ = w.Write(outputBytes)
}

// getContentType returns the MIME standard content-type string for formats.
func getContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		return "image/png"
	case "bmp":
		return "image/bmp"
	case "epd_raw":
		return "application/octet-stream"
	default:
		return "image/png"
	}
}
