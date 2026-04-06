package viamscales

import (
	"context"
	"fmt"
	"math"
	"sync"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	rdkutils "go.viam.com/rdk/utils"
)

var (
	MultiLoadCellModel = resource.NewModel("erh", "viam-scales", "multi-load-cell")
)

func init() {
	resource.RegisterComponent(sensor.API, MultiLoadCellModel,
		resource.Registration[sensor.Sensor, *MultiLoadCellConfig]{
			Constructor: newMultiLoadCell,
		},
	)
}

type LoadCellEntry struct {
	Sensor               string  `json:"sensor"`
	DistanceFromCenterMm float64 `json:"distance_from_center_mm"`
	DirectionDegrees     float64 `json:"direction_degrees"`
}

type MultiLoadCellConfig struct {
	Sensors []LoadCellEntry `json:"sensors"`
}

func (cfg *MultiLoadCellConfig) Validate(path string) ([]string, []string, error) {
	if len(cfg.Sensors) == 0 {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "sensors")
	}
	deps := make([]string, 0, len(cfg.Sensors))
	for i, entry := range cfg.Sensors {
		if entry.Sensor == "" {
			return nil, nil, resource.NewConfigValidationFieldRequiredError(path, fmt.Sprintf("sensors[%d].sensor", i))
		}
		deps = append(deps, entry.Sensor)
	}
	return deps, nil, nil
}

type loadCellInfo struct {
	sensor               sensor.Sensor
	distanceFromCenterMm float64
	directionRadians     float64
}

type MultiLoadCell struct {
	resource.AlwaysRebuild

	name   resource.Name
	logger logging.Logger

	mu    sync.Mutex
	cells []loadCellInfo
}

func newMultiLoadCell(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*MultiLoadCellConfig](rawConf)
	if err != nil {
		return nil, err
	}
	return NewMultiLoadCell(ctx, deps, rawConf.ResourceName(), conf, logger)
}

func NewMultiLoadCell(ctx context.Context, deps resource.Dependencies, name resource.Name, conf *MultiLoadCellConfig, logger logging.Logger) (sensor.Sensor, error) {
	cells := make([]loadCellInfo, 0, len(conf.Sensors))
	for _, entry := range conf.Sensors {
		s, err := sensor.FromDependencies(deps, entry.Sensor)
		if err != nil {
			return nil, fmt.Errorf("failed to find sensor %q: %w", entry.Sensor, err)
		}
		cells = append(cells, loadCellInfo{
			sensor:               s,
			distanceFromCenterMm: entry.DistanceFromCenterMm,
			directionRadians:   entry.DirectionDegrees * math.Pi / 180.0,
		})
	}

	return &MultiLoadCell{
		name:   name,
		logger: logger,
		cells:  cells,
	}, nil
}

func (m *MultiLoadCell) Name() resource.Name {
	return m.name
}

func (m *MultiLoadCell) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	totalKg := 0.0
	totalN := 0.0
	vecX := 0.0
	vecY := 0.0
	hasCalibrated := false

	for _, cell := range m.cells {
		readings, err := cell.sensor.Readings(ctx, extra)
		if err != nil {
			return nil, fmt.Errorf("error reading sensor: %w", err)
		}

		kg, kgOk := toOptionalFloat64(readings["weight_kg"])
		n, nOk := toOptionalFloat64(readings["force_N"])

		if kgOk {
			hasCalibrated = true
			totalKg += kg
			totalN += n

			// Use force at this cell's position to compute direction vector.
			// A cell reading more weight means force is applied near/toward that cell.
			force := kg
			if cell.distanceFromCenterMm > 0 {
				vecX += force * cell.distanceFromCenterMm * math.Cos(cell.directionRadians)
				vecY += force * cell.distanceFromCenterMm * math.Sin(cell.directionRadians)
			}
		}
		_ = nOk
	}

	result := map[string]interface{}{}

	if hasCalibrated {
		result["total_weight_kg"] = totalKg
		result["total_force_N"] = totalN

		magnitude := math.Sqrt(vecX*vecX + vecY*vecY)
		if magnitude > 0 && totalKg > 0 {
			dirDeg := math.Atan2(vecY, vecX) * 180.0 / math.Pi
			if dirDeg < 0 {
				dirDeg += 360
			}
			result["force_direction_degrees"] = dirDeg
			result["force_center_offset_ratio"] = magnitude / totalKg
		}
	}

	return result, nil
}

func toOptionalFloat64(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	f, err := toFloat64(v)
	if err != nil {
		return 0, false
	}
	return f, true
}

func (m *MultiLoadCell) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := cmd["tare"]; ok {
		results := map[string]interface{}{}
		for i, cell := range m.cells {
			resp, err := cell.sensor.DoCommand(ctx, map[string]interface{}{"tare": true})
			if err != nil {
				return nil, fmt.Errorf("tare failed on sensor %d: %w", i, err)
			}
			results[fmt.Sprintf("sensor_%d", i)] = resp
		}
		return results, nil
	}

	return nil, fmt.Errorf("unknown command, supported commands: tare")
}

func (m *MultiLoadCell) Close(context.Context) error {
	return nil
}
