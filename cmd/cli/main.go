package main

import (
	"context"
	"fmt"
	"viamscales"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"

	sensor "go.viam.com/rdk/components/sensor"
)

func main() {
	err := realMain()
	if err != nil {
		panic(err)
	}
}

func realMain() error {
	ctx := context.Background()
	logger := logging.NewLogger("cli")

	cfg := viamscales.ConfigurableScaleConfig{
		Sensor: "my-loadcell",
	}

	// In real usage, deps would contain the underlying sensor from a Viam robot.
	deps := resource.Dependencies{
		// sensor.Named("my-loadcell"): myUnderlyingSensor,
	}

	thing, err := viamscales.NewConfigurableScale(ctx, deps, sensor.Named("foo"), &cfg, logger)
	if err != nil {
		fmt.Printf("expected error without real sensor dependency: %v\n", err)
		return nil
	}
	defer thing.Close(ctx)

	return nil
}
