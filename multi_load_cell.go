package viamscales

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var (
	MultiLoadCellModel = resource.NewModel("erh", "viam-scales", "multi-load-cell")
)

// Background polling constants.
const (
	// loadCellPollInterval targets 50Hz. Goroutines read as fast as the
	// underlying sensor allows, up to this cap.
	loadCellPollInterval = 20 * time.Millisecond
	// loadCellMaxAge is how stale a cached reading may be before Readings
	// returns an error.
	loadCellMaxAge = 500 * time.Millisecond
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
	sensorName           string
	distanceFromCenterMm float64
	directionRadians     float64

	mu             sync.Mutex
	latestReadings map[string]interface{}
	latestErr      error
	latestTime     time.Time
}

// readOnce reads the underlying sensor once and updates the cache.
// On success, latestReadings and latestTime are updated. On error, latestErr
// is set but the previous latestReadings and latestTime remain so transient
// errors don't immediately invalidate fresh data; the staleness check in
// Readings is the source of truth.
func (c *loadCellInfo) readOnce(ctx context.Context) {
	readings, err := c.sensor.Readings(ctx, nil)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.latestErr = err
		return
	}
	c.latestReadings = readings
	c.latestErr = nil
	c.latestTime = now
}

// latest returns the most recent cached reading along with the time it was
// taken. If no successful read has happened yet, the last error is returned.
func (c *loadCellInfo) latest() (map[string]interface{}, time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latestReadings == nil {
		if c.latestErr != nil {
			return nil, time.Time{}, c.latestErr
		}
		return nil, time.Time{}, fmt.Errorf("no reading available yet for sensor %q", c.sensorName)
	}
	return c.latestReadings, c.latestTime, nil
}

func (c *loadCellInfo) poll(ctx context.Context, logger logging.Logger) {
	ticker := time.NewTicker(loadCellPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.readOnce(ctx)
		}
	}
}

type MultiLoadCell struct {
	resource.AlwaysRebuild

	name   resource.Name
	logger logging.Logger

	mu sync.Mutex

	cells    []*loadCellInfo
	cancelFn context.CancelFunc
	wg       sync.WaitGroup
}

func newMultiLoadCell(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*MultiLoadCellConfig](rawConf)
	if err != nil {
		return nil, err
	}
	return NewMultiLoadCell(ctx, deps, rawConf.ResourceName(), conf, logger)
}

func NewMultiLoadCell(ctx context.Context, deps resource.Dependencies, name resource.Name, conf *MultiLoadCellConfig, logger logging.Logger) (sensor.Sensor, error) {
	cells := make([]*loadCellInfo, 0, len(conf.Sensors))
	for _, entry := range conf.Sensors {
		s, err := sensor.FromDependencies(deps, entry.Sensor)
		if err != nil {
			return nil, fmt.Errorf("failed to find sensor %q: %w", entry.Sensor, err)
		}
		cells = append(cells, &loadCellInfo{
			sensor:               s,
			sensorName:           entry.Sensor,
			distanceFromCenterMm: entry.DistanceFromCenterMm,
			directionRadians:     entry.DirectionDegrees * math.Pi / 180.0,
		})
	}

	// Prime the cache synchronously so a call to Readings() immediately after
	// construction has data to work with.
	for _, cell := range cells {
		cell.readOnce(ctx)
	}

	pollCtx, cancel := context.WithCancel(context.Background())

	m := &MultiLoadCell{
		name:     name,
		logger:   logger,
		cells:    cells,
		cancelFn: cancel,
	}

	for _, cell := range cells {
		c := cell
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			c.poll(pollCtx, logger)
		}()
	}

	return m, nil
}

func (m *MultiLoadCell) Name() resource.Name {
	return m.name
}

func (m *MultiLoadCell) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	totalKg := 0.0
	totalN := 0.0
	vecX := 0.0
	vecY := 0.0
	hasCalibrated := false

	raw := []interface{}{}

	now := time.Now()

	for _, cell := range m.cells {
		readings, lastRead, err := cell.latest()
		if err != nil {
			return nil, fmt.Errorf("error reading sensor %q: %w", cell.sensorName, err)
		}
		age := now.Sub(lastRead)
		if age > loadCellMaxAge {
			return nil, fmt.Errorf("latest reading for sensor %q is too stale (age %v > %v)", cell.sensorName, age, loadCellMaxAge)
		}

		raw = append(raw, readings)

		kg, kgOk := toOptionalFloat64(readings["weight_kg"])
		n, nOk := toOptionalFloat64(readings["force_N"])

		kg = max(0, kg) // questionable?

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
	if extra != nil && extra["debug"] == true {
		result["raw"] = raw
	}

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

func (m *MultiLoadCell) Status(ctx context.Context) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]interface{}{
		"num_cells": len(m.cells),
	}, nil
}

func (m *MultiLoadCell) Close(context.Context) error {
	if m.cancelFn != nil {
		m.cancelFn()
	}
	m.wg.Wait()
	return nil
}
