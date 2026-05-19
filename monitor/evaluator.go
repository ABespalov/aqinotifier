package monitor

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/rs/zerolog/log"
)

type CompiledFormula struct {
	Target  string
	Program *vm.Program
}

type DeviceEvaluator struct {
	Formulas []CompiledFormula
}

var identRegex = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\b`)

func parseTernary(f string) string {
	f = strings.TrimSpace(f)
	if strings.HasPrefix(f, "?") {
		parts := strings.Split(f[1:], ":")
		if len(parts) == 3 {
			return fmt.Sprintf("(%s) ? (%s) : (%s)", parts[0], parts[1], parts[2])
		}
	}
	return f
}

func buildEvaluator(corrections map[string]string) (*DeviceEvaluator, error) {
	if len(corrections) == 0 {
		return &DeviceEvaluator{}, nil
	}

	// 1. Dependency graph
	deps := make(map[string][]string)
	for target, formula := range corrections {
		if strings.TrimSpace(formula) == "" {
			continue
		}
		formula = parseTernary(formula)
		matches := identRegex.FindAllStringSubmatch(formula, -1)

		var dependsOn []string
		for _, m := range matches {
			v := m[1]
			// Only consider dependencies on other computed fields
			if _, exists := corrections[v]; exists && v != target {
				dependsOn = append(dependsOn, v)
			}
		}
		deps[target] = dependsOn
	}

	// 2. Topological sort
	var sorted []string
	visited := make(map[string]bool)
	tempMark := make(map[string]bool)

	var visit func(string) error
	visit = func(n string) error {
		if tempMark[n] {
			return fmt.Errorf("circular dependency detected involving %s", n)
		}
		if !visited[n] {
			tempMark[n] = true
			for _, dep := range deps[n] {
				if err := visit(dep); err != nil {
					return err
				}
			}
			tempMark[n] = false
			visited[n] = true
			sorted = append(sorted, n)
		}
		return nil
	}

	// To ensure determinism in sort, sort keys first
	var keys []string
	for k := range corrections {
		keys = append(keys, k)
	}

	for _, k := range keys {
		if !visited[k] {
			if err := visit(k); err != nil {
				return nil, err
			}
		}
	}

	// 3. Compile formulas in sorted order
	env := map[string]interface{}{
		"pm10":            0.0,
		"pm25":            0.0,
		"pm10_raw":        0.0,
		"pm25_raw":        0.0,
		"temperature":     0.0,
		"humidity":        0.0,
		"pressure":        0.0,
		"pm03":            0.0,
		"pm03_raw":        0.0,
		"pm01":            0.0,
		"pm01_raw":        0.0,
		"co2":             0.0,
		"co2_raw":         0.0,
		"tvoc":            0.0,
		"tvoc_raw":        0.0,
		"nox":             0.0,
		"nox_raw":         0.0,
		"temperature_raw": 0.0,
		"humidity_raw":    0.0,
		"pressure_raw":    0.0,
	}

	var compiled []CompiledFormula
	for _, target := range sorted {
		rawFormula := corrections[target]
		if strings.TrimSpace(rawFormula) == "" {
			continue
		}
		formula := parseTernary(rawFormula)
		program, err := expr.Compile(formula, expr.Env(env))
		if err != nil {
			return nil, fmt.Errorf("failed to compile formula for %s: %w", target, err)
		}
		compiled = append(compiled, CompiledFormula{
			Target:  target,
			Program: program,
		})
	}

	return &DeviceEvaluator{Formulas: compiled}, nil
}

func (e *DeviceEvaluator) Evaluate(m *Measurement) {
	if len(e.Formulas) == 0 {
		return
	}

	env := map[string]interface{}{
		"pm10":            m.PM10,
		"pm25":            m.PM25,
		"pm10_raw":        m.PM10Raw,
		"pm25_raw":        m.PM25Raw,
		"temperature":     m.Temperature,
		"humidity":        m.Humidity,
		"pressure":        m.Pressure,
		"pm03":            m.PM03,
		"pm03_raw":        m.PM03Raw,
		"pm01":            m.PM01,
		"pm01_raw":        m.PM01Raw,
		"co2":             m.CO2,
		"co2_raw":         m.CO2Raw,
		"tvoc":            m.TVOC,
		"tvoc_raw":        m.TVOCRaw,
		"nox":             m.Nox,
		"nox_raw":         m.NoxRaw,
		"temperature_raw": m.TemperatureRaw,
		"humidity_raw":    m.HumidityRaw,
		"pressure_raw":    m.PressureRaw,
	}

	for _, f := range e.Formulas {
		out, err := expr.Run(f.Program, env)
		if err != nil {
			log.Error().Err(err).Str("target", f.Target).Msg("failed to evaluate formula")
			continue
		}
		val, ok := out.(float64)
		if !ok {
			// Try type conversion just in case
			switch v := out.(type) {
			case int:
				val = float64(v)
			case float32:
				val = float64(v)
			default:
				log.Error().Str("target", f.Target).Msgf("formula result is not a number: %T", out)
				continue
			}
		}

		// Update both struct and environment for downstream formulas
		switch f.Target {
		case "pm10":
			m.PM10 = val
		case "pm25":
			m.PM25 = val
		case "temperature":
			m.Temperature = val
		case "humidity":
			m.Humidity = val
		case "pressure":
			m.Pressure = val
		case "pm03":
			m.PM03 = val
		case "pm01":
			m.PM01 = val
		case "co2":
			m.CO2 = val
		case "tvoc":
			m.TVOC = val
		case "nox":
			m.Nox = val
		}
		env[f.Target] = val
	}
}
