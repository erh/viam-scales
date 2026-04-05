# Module viam-scales 

A Viam sensor module that wraps any raw sensor and adds tare, calibration, and weight conversion. Use it to turn a raw ADC sensor (like an HX711 load cell) into a calibrated scale that reports weight in kg and force in N.

## Models

This module provides the following model(s):

- [`erh:viam-scales:configurable-scale`](erh_viam-scales_configurable-scale.md) - A configurable scale that reads from an underlying sensor and applies tare offset and calibration
