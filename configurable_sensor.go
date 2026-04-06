package viamscales

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

var (
	ConfigurableScaleModel = resource.NewModel("erh", "viam-scales", "configurable-scale")
)

func init() {
	resource.RegisterComponent(sensor.API, ConfigurableScaleModel,
		resource.Registration[sensor.Sensor, *ConfigurableScaleConfig]{
			Constructor: newViamScalesConfigurableScale,
		},
	)
}

type ConfigurableScaleConfig struct {
	Sensor           string  `json:"sensor"`
	SensorField      string  `json:"sensor_field,omitempty"`
	CalibrationSlope float64 `json:"calibration_slope,omitempty"`
	Offset           float64 `json:"offset,omitempty"`
	TareSamples      int     `json:"tare_samples,omitempty"`
}

func (cfg *ConfigurableScaleConfig) Validate(path string) ([]string, []string, error) {
	if cfg.Sensor == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "sensor")
	}
	return []string{cfg.Sensor}, nil, nil
}

type ConfigurableScale struct {
	resource.AlwaysRebuild

	name   resource.Name
	logger logging.Logger
	cfg    *ConfigurableScaleConfig

	mu               sync.Mutex
	underlying       sensor.Sensor
	offset           float64
	calibrationSlope float64
}

func newViamScalesConfigurableScale(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (sensor.Sensor, error) {
	conf, err := resource.NativeConfig[*ConfigurableScaleConfig](rawConf)
	if err != nil {
		return nil, err
	}
	return NewConfigurableScale(ctx, deps, rawConf.ResourceName(), conf, logger)
}

func NewConfigurableScale(ctx context.Context, deps resource.Dependencies, name resource.Name, conf *ConfigurableScaleConfig, logger logging.Logger) (sensor.Sensor, error) {

	s := &ConfigurableScale{
		name:             name,
		logger:           logger,
		cfg:              conf,
		offset:           conf.Offset,
		calibrationSlope: conf.CalibrationSlope,
	}

	if conf.Sensor == "" {
		return nil, fmt.Errorf("no sensor")
	}

	underlying, err := sensor.FromDependencies(deps, conf.Sensor)
	if err != nil {
		return nil, fmt.Errorf("failed to find underlying sensor %q: %w", conf.Sensor, err)
	}
	s.underlying = underlying

	return s, nil
}

func (s *ConfigurableScale) Name() resource.Name {
	return s.name
}

func toFloat64(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case int32:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}

// readRawValue reads a single raw value from the underlying sensor.
func (s *ConfigurableScale) readRawValue(ctx context.Context) (float64, error) {
	readings, err := s.underlying.Readings(ctx, nil)
	if err != nil {
		return 0, err
	}

	if s.cfg.SensorField != "" {
		v, ok := readings[s.cfg.SensorField]
		if !ok {
			return 0, fmt.Errorf("underlying sensor missing %q reading", s.cfg.SensorField)
		}
		return toFloat64(v)
	}

	for _, v := range readings {
		f, err := toFloat64(v)
		if err == nil {
			return f, nil
		}
	}

	return 0, fmt.Errorf("no numeric value found in underlying sensor readings")
}

// readAverage reads n samples from the underlying sensor and returns a trimmed mean,
// removing 20%% of outliers from each end.
func (s *ConfigurableScale) readAverage(ctx context.Context, n int) (float64, error) {
	if n <= 0 {
		n = 15
	}

	samples := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		val, err := s.readRawValue(ctx)
		if err != nil {
			return 0, fmt.Errorf("error reading sample %d: %w", i, err)
		}
		samples = append(samples, val)
	}

	sort.Float64s(samples)

	trimCount := int(math.Round(float64(n) * 0.2))
	if trimCount*2 >= n {
		trimCount = 0
	}
	trimmed := samples[trimCount : n-trimCount]

	sum := 0.0
	for _, v := range trimmed {
		sum += v
	}
	return sum / float64(len(trimmed)), nil
}

func (s *ConfigurableScale) Readings(ctx context.Context, extra map[string]interface{}) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := s.readRawValue(ctx)
	if err != nil {
		return nil, err
	}

	adjusted := raw - s.offset

	result := map[string]interface{}{
		"raw_value": raw,
	}

	if s.calibrationSlope != 0 {
		weightKg := adjusted / s.calibrationSlope
		result["weight_kg"] = weightKg
		result["force_N"] = weightKg * 9.81
	}

	return result, nil
}

func (s *ConfigurableScale) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := cmd["tare"]; ok {
		n := s.cfg.TareSamples
		if n <= 0 {
			n = 15
		}
		avg, err := s.readAverage(ctx, n)
		if err != nil {
			return nil, fmt.Errorf("tare failed: %w", err)
		}
		s.offset = avg
		s.logger.Infof("tare complete, offset: %f", s.offset)
		return map[string]interface{}{
			"offset": s.offset,
		}, nil
	}

	if val, ok := cmd["calibrate_kg"]; ok {
		knownWeight, ok := val.(float64)
		if !ok || knownWeight == 0 {
			return nil, fmt.Errorf("calibrate_kg requires a non-zero numeric weight value")
		}
		n := s.cfg.TareSamples
		if n <= 0 {
			n = 15
		}
		avg, err := s.readAverage(ctx, n)
		if err != nil {
			return nil, fmt.Errorf("calibration failed: %w", err)
		}
		s.calibrationSlope = (avg - s.offset) / knownWeight
		s.logger.Infof("calibration complete, slope: %f", s.calibrationSlope)
		return map[string]interface{}{
			"calibration_slope": s.calibrationSlope,
		}, nil
	}

	if _, ok := cmd["get_calibration"]; ok {
		return map[string]interface{}{
			"offset":            s.offset,
			"calibration_slope": s.calibrationSlope,
		}, nil
	}

	return nil, fmt.Errorf("unknown command, supported commands: tare, calibrate_kg, get_calibration")
}

func (s *ConfigurableScale) Status(ctx context.Context) (map[string]interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]interface{}{
		"offset":            s.offset,
		"calibration_slope": s.calibrationSlope,
	}, nil
}

func (s *ConfigurableScale) Close(context.Context) error {
	return nil
}
