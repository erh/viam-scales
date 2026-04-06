package main

import (
	sensor "go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"viamscales"
)

func main() {
	// ModularMain can take multiple APIModel arguments, if your module implements multiple models.
	module.ModularMain(
		resource.APIModel{sensor.API, viamscales.ConfigurableScaleModel},
		resource.APIModel{sensor.API, viamscales.MultiLoadCellModel},
	)
}
