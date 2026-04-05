# Model erh:viam-scales:configurable-scale

A configurable scale sensor that wraps any underlying sensor providing raw numeric readings. It handles tare (zeroing), calibration, and conversion to weight (kg) and force (N).

## Configuration

```json
{
  "sensor": "<string>",
  "sensor_field": "<string>",
  "calibration_slope": <float>,
  "offset": <float>,
  "tare_samples": <int>
}
```

### Attributes

| Name                | Type   | Inclusion | Description                                                                 |
|---------------------|--------|-----------|-----------------------------------------------------------------------------|
| `sensor`            | string | Required  | Name of the underlying sensor component to read raw values from             |
| `sensor_field`      | string | Optional  | Specific field name to read from the underlying sensor's readings. If not set, uses the first numeric value |
| `calibration_slope` | float  | Optional  | Pre-stored calibration factor (raw units per kg)                            |
| `offset`            | float  | Optional  | Pre-stored tare offset                                                      |
| `tare_samples`      | int    | Optional  | Number of samples to average during tare/calibration (default: 15)          |

### Example Configuration

```json
{
  "sensor": "my-loadcell",
  "sensor_field": "raw_value",
  "calibration_slope": 2000.0,
  "offset": 1000.0,
  "tare_samples": 15
}
```

## Readings

| Name        | Type  | Condition         | Description                            |
|-------------|-------|-------------------|----------------------------------------|
| `raw_value` | float | Always            | Raw reading from the underlying sensor |
| `weight_kg` | float | When calibrated   | `(raw - offset) / calibration_slope`   |
| `force_N`   | float | When calibrated   | `weight_kg * 9.81`                     |

## DoCommand

### `tare`

Zeros the scale by averaging multiple samples and storing the result as the offset.

```json
{"tare": true}
```

Returns:
```json
{"offset": 1000.0}
```

### `calibrate_kg`

Calibrates using a known weight. Place the weight on the scale, then send this command. The scale must be tared first.

```json
{"calibrate_kg": 2.0}
```

Returns:
```json
{"calibration_slope": 2000.0}
```

### `get_calibration`

Returns the current calibration values.

```json
{"get_calibration": true}
```

Returns:
```json
{"offset": 1000.0, "calibration_slope": 2000.0}
```
