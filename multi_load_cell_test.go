package viamscales

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

type fakeCalibratedSensor struct {
	resource.AlwaysRebuild
	name     resource.Name
	weightKg float64
	forceN   float64

	mu     sync.Mutex
	tared  bool
	broken bool
}

func (f *fakeCalibratedSensor) setBroken(b bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken = b
}

func (f *fakeCalibratedSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	f.mu.Lock()
	broken := f.broken
	f.mu.Unlock()
	if broken {
		return nil, fmt.Errorf("sensor is broken")
	}
	return map[string]interface{}{
		"weight_kg": f.weightKg,
		"force_N":   f.forceN,
	}, nil
}

func (f *fakeCalibratedSensor) Name() resource.Name         { return f.name }
func (f *fakeCalibratedSensor) Close(context.Context) error { return nil }
func (f *fakeCalibratedSensor) Status(ctx context.Context) (map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeCalibratedSensor) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := cmd["tare"]; ok {
		f.mu.Lock()
		f.tared = true
		f.mu.Unlock()
		return map[string]interface{}{"offset": 0.0}, nil
	}
	return nil, nil
}

func (f *fakeCalibratedSensor) wasTared() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tared
}

func TestMultiLoadCellReadingsSum(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")

	s1 := &fakeCalibratedSensor{name: sensor.Named("s1"), weightKg: 3.0, forceN: 3.0 * 9.81}
	s2 := &fakeCalibratedSensor{name: sensor.Named("s2"), weightKg: 5.0, forceN: 5.0 * 9.81}

	deps := resource.Dependencies{
		sensor.Named("s1"): s1,
		sensor.Named("s2"): s2,
	}
	cfg := &MultiLoadCellConfig{
		Sensors: []LoadCellEntry{
			{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
			{Sensor: "s2", DistanceFromCenterMm: 1.0, DirectionDegrees: 180},
		},
	}

	m, err := NewMultiLoadCell(ctx, deps, sensor.Named("multi"), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)

	readings, err := m.Readings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	totalKg, ok := readings["total_weight_kg"].(float64)
	if !ok {
		t.Fatal("total_weight_kg missing")
	}
	if totalKg != 8.0 {
		t.Fatalf("expected total_weight_kg 8.0, got %v", totalKg)
	}

	totalN, ok := readings["total_force_N"].(float64)
	if !ok {
		t.Fatal("total_force_N missing")
	}
	expectedN := 8.0 * 9.81
	if math.Abs(totalN-expectedN) > 0.0001 {
		t.Fatalf("expected total_force_N %f, got %f", expectedN, totalN)
	}
}

func TestMultiLoadCellForceDirection(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")

	// All force on 90-degree sensor => direction should be 90
	s1 := &fakeCalibratedSensor{name: sensor.Named("s1"), weightKg: 0.0, forceN: 0.0}
	s2 := &fakeCalibratedSensor{name: sensor.Named("s2"), weightKg: 10.0, forceN: 10.0 * 9.81}

	deps := resource.Dependencies{
		sensor.Named("s1"): s1,
		sensor.Named("s2"): s2,
	}
	cfg := &MultiLoadCellConfig{
		Sensors: []LoadCellEntry{
			{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
			{Sensor: "s2", DistanceFromCenterMm: 1.0, DirectionDegrees: 90},
		},
	}

	m, err := NewMultiLoadCell(ctx, deps, sensor.Named("multi"), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)

	readings, err := m.Readings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	dir, ok := readings["force_direction_degrees"].(float64)
	if !ok {
		t.Fatal("force_direction_degrees missing")
	}
	if math.Abs(dir-90.0) > 0.01 {
		t.Fatalf("expected direction ~90, got %f", dir)
	}
}

func TestMultiLoadCellEvenForceNoDirection(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")

	// Equal force on opposite sensors => vectors cancel, no direction
	s1 := &fakeCalibratedSensor{name: sensor.Named("s1"), weightKg: 5.0, forceN: 5.0 * 9.81}
	s2 := &fakeCalibratedSensor{name: sensor.Named("s2"), weightKg: 5.0, forceN: 5.0 * 9.81}

	deps := resource.Dependencies{
		sensor.Named("s1"): s1,
		sensor.Named("s2"): s2,
	}
	cfg := &MultiLoadCellConfig{
		Sensors: []LoadCellEntry{
			{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
			{Sensor: "s2", DistanceFromCenterMm: 1.0, DirectionDegrees: 180},
		},
	}

	m, err := NewMultiLoadCell(ctx, deps, sensor.Named("multi"), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)

	readings, err := m.Readings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Vectors cancel out, so direction and offset should not be present
	if _, ok := readings["force_direction_degrees"]; ok {
		// magnitude is effectively 0, so this should not appear
		offset := readings["force_center_offset_ratio"].(float64)
		if offset > 0.001 {
			t.Fatalf("expected no direction for balanced load, got offset %f", offset)
		}
	}
}

func TestMultiLoadCellTare(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")

	s1 := &fakeCalibratedSensor{name: sensor.Named("s1"), weightKg: 1.0, forceN: 9.81}
	s2 := &fakeCalibratedSensor{name: sensor.Named("s2"), weightKg: 2.0, forceN: 2.0 * 9.81}

	deps := resource.Dependencies{
		sensor.Named("s1"): s1,
		sensor.Named("s2"): s2,
	}
	cfg := &MultiLoadCellConfig{
		Sensors: []LoadCellEntry{
			{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
			{Sensor: "s2", DistanceFromCenterMm: 1.0, DirectionDegrees: 90},
		},
	}

	m, err := NewMultiLoadCell(ctx, deps, sensor.Named("multi"), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)

	_, err = m.DoCommand(ctx, map[string]interface{}{"tare": true})
	if err != nil {
		t.Fatal(err)
	}

	if !s1.wasTared() {
		t.Fatal("expected s1 to be tared")
	}
	if !s2.wasTared() {
		t.Fatal("expected s2 to be tared")
	}
}

func TestMultiLoadCellStaleReading(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")

	s1 := &fakeCalibratedSensor{name: sensor.Named("s1"), weightKg: 3.0, forceN: 3.0 * 9.81}
	s2 := &fakeCalibratedSensor{name: sensor.Named("s2"), weightKg: 5.0, forceN: 5.0 * 9.81}

	deps := resource.Dependencies{
		sensor.Named("s1"): s1,
		sensor.Named("s2"): s2,
	}
	cfg := &MultiLoadCellConfig{
		Sensors: []LoadCellEntry{
			{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
			{Sensor: "s2", DistanceFromCenterMm: 1.0, DirectionDegrees: 180},
		},
	}

	m, err := NewMultiLoadCell(ctx, deps, sensor.Named("multi"), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)

	// Initially readings work fine.
	if _, err := m.Readings(ctx, nil); err != nil {
		t.Fatalf("expected fresh readings, got err: %v", err)
	}

	// Break s2 so its cache stops advancing.
	s2.setBroken(true)

	// Wait long enough for s2's cached reading to exceed loadCellMaxAge.
	time.Sleep(loadCellMaxAge + 100*time.Millisecond)

	_, err = m.Readings(ctx, nil)
	if err == nil {
		t.Fatal("expected stale-reading error, got nil")
	}
	if !strings.Contains(err.Error(), "too stale") {
		t.Fatalf("expected stale error, got: %v", err)
	}

	// Recover the sensor; the next poll should refresh the cache and Readings succeeds again.
	s2.setBroken(false)
	time.Sleep(loadCellPollInterval * 3)

	if _, err := m.Readings(ctx, nil); err != nil {
		t.Fatalf("expected readings to recover after sensor unblocked, got err: %v", err)
	}
}

func TestMultiLoadCellBackgroundPolling(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")

	s1 := &fakeCalibratedSensor{name: sensor.Named("s1"), weightKg: 1.0, forceN: 9.81}

	deps := resource.Dependencies{
		sensor.Named("s1"): s1,
	}
	cfg := &MultiLoadCellConfig{
		Sensors: []LoadCellEntry{
			{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
		},
	}

	m, err := NewMultiLoadCell(ctx, deps, sensor.Named("multi"), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)

	// Cast to concrete type to inspect cache timestamps directly.
	mlc := m.(*MultiLoadCell)

	_, firstTime, err := mlc.cells[0].latest()
	if err != nil {
		t.Fatal(err)
	}

	// Wait enough for multiple poll ticks.
	time.Sleep(loadCellPollInterval * 5)

	_, secondTime, err := mlc.cells[0].latest()
	if err != nil {
		t.Fatal(err)
	}

	if !secondTime.After(firstTime) {
		t.Fatalf("expected background poll to refresh latestTime; first=%v second=%v", firstTime, secondTime)
	}
}

func TestMultiLoadCellValidate(t *testing.T) {
	cfg := &MultiLoadCellConfig{}
	_, _, err := cfg.Validate("test")
	if err == nil {
		t.Fatal("expected error for empty sensors")
	}

	cfg.Sensors = []LoadCellEntry{{Sensor: ""}}
	_, _, err = cfg.Validate("test")
	if err == nil {
		t.Fatal("expected error for empty sensor name")
	}

	cfg.Sensors = []LoadCellEntry{
		{Sensor: "s1", DistanceFromCenterMm: 1.0, DirectionDegrees: 0},
		{Sensor: "s2", DistanceFromCenterMm: 1.0, DirectionDegrees: 90},
	}
	deps, _, err := cfg.Validate("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[0] != "s1" || deps[1] != "s2" {
		t.Fatalf("expected deps [s1 s2], got %v", deps)
	}
}
