package viamscales

import (
	"context"
	"testing"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

type fakeSensor struct {
	resource.AlwaysRebuild
	name  resource.Name
	value float64
}

func (f *fakeSensor) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"raw_value": f.value}, nil
}

func (f *fakeSensor) Name() resource.Name {
	return f.name
}

func (f *fakeSensor) Close(ctx context.Context) error { return nil }

func (f *fakeSensor) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, nil
}

func (f *fakeSensor) Status(ctx context.Context) (map[string]interface{}, error) {
	return nil, nil
}

func TestReadingsRaw(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")
	fake := &fakeSensor{name: sensor.Named("fake"), value: 1000}

	deps := resource.Dependencies{
		sensor.Named("fake"): fake,
	}
	cfg := &ConfigurableScaleConfig{Sensor: "fake"}

	s, err := NewConfigurableScale(ctx, deps, sensor.Named("test-scale"), cfg, nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	readings, err := s.Readings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	if readings["raw_value"] != 1000.0 {
		t.Fatalf("expected raw_value 1000, got %v", readings["raw_value"])
	}
	if _, ok := readings["weight_kg"]; ok {
		t.Fatal("weight_kg should not be present without calibration")
	}
}

func TestReadingsCalibrated(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")
	fake := &fakeSensor{name: sensor.Named("fake"), value: 5000}

	deps := resource.Dependencies{
		sensor.Named("fake"): fake,
	}
	cfg := &ConfigurableScaleConfig{
		Sensor:           "fake",
		Offset:           1000,
		CalibrationSlope: 2000, // 2000 raw units per kg
	}

	s, err := NewConfigurableScale(ctx, deps, sensor.Named("test-scale"), cfg, nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	readings, err := s.Readings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	weightKg, ok := readings["weight_kg"].(float64)
	if !ok {
		t.Fatal("weight_kg missing or not float64")
	}
	// (5000 - 1000) / 2000 = 2.0 kg
	if weightKg != 2.0 {
		t.Fatalf("expected weight_kg 2.0, got %v", weightKg)
	}

	forceN, ok := readings["force_N"].(float64)
	if !ok {
		t.Fatal("force_N missing or not float64")
	}
	expected := 2.0 * 9.81
	if forceN != expected {
		t.Fatalf("expected force_N %f, got %f", expected, forceN)
	}
}

func TestTare(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")
	fake := &fakeSensor{name: sensor.Named("fake"), value: 500}

	deps := resource.Dependencies{
		sensor.Named("fake"): fake,
	}
	cfg := &ConfigurableScaleConfig{Sensor: "fake", TareSamples: 5}

	s, err := NewConfigurableScale(ctx, deps, sensor.Named("test-scale"), cfg, nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.DoCommand(ctx, map[string]interface{}{"tare": true})
	if err != nil {
		t.Fatal(err)
	}

	offset, ok := result["offset"].(float64)
	if !ok {
		t.Fatal("offset missing or not float64")
	}
	if offset != 500.0 {
		t.Fatalf("expected offset 500, got %v", offset)
	}
}

func TestCalibrateKg(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")
	fake := &fakeSensor{name: sensor.Named("fake"), value: 3000}

	deps := resource.Dependencies{
		sensor.Named("fake"): fake,
	}
	cfg := &ConfigurableScaleConfig{Sensor: "fake", TareSamples: 5}

	s, err := NewConfigurableScale(ctx, deps, sensor.Named("test-scale"), cfg, nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	// First tare at 1000
	fake.value = 1000
	_, err = s.DoCommand(ctx, map[string]interface{}{"tare": true})
	if err != nil {
		t.Fatal(err)
	}

	// Place 2kg known weight, sensor reads 5000
	fake.value = 5000
	result, err := s.DoCommand(ctx, map[string]interface{}{"calibrate_kg": 2.0})
	if err != nil {
		t.Fatal(err)
	}

	slope, ok := result["calibration_slope"].(float64)
	if !ok {
		t.Fatal("calibration_slope missing or not float64")
	}
	// (5000 - 1000) / 2.0 = 2000
	if slope != 2000.0 {
		t.Fatalf("expected slope 2000, got %v", slope)
	}
}

func TestGetCalibration(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger("test")
	fake := &fakeSensor{name: sensor.Named("fake"), value: 0}

	deps := resource.Dependencies{
		sensor.Named("fake"): fake,
	}
	cfg := &ConfigurableScaleConfig{
		Sensor:           "fake",
		Offset:           100,
		CalibrationSlope: 200,
	}

	s, err := NewConfigurableScale(ctx, deps, sensor.Named("test-scale"), cfg, nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.DoCommand(ctx, map[string]interface{}{"get_calibration": true})
	if err != nil {
		t.Fatal(err)
	}

	if result["offset"] != 100.0 {
		t.Fatalf("expected offset 100, got %v", result["offset"])
	}
	if result["calibration_slope"] != 200.0 {
		t.Fatalf("expected slope 200, got %v", result["calibration_slope"])
	}
}

func TestValidate(t *testing.T) {
	cfg := &ConfigurableScaleConfig{}
	_, _, err := cfg.Validate("test")
	if err == nil {
		t.Fatal("expected error for missing sensor")
	}

	cfg.Sensor = "my-sensor"
	deps, _, err := cfg.Validate("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0] != "my-sensor" {
		t.Fatalf("expected deps [my-sensor], got %v", deps)
	}
}
