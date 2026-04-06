# Model erh:viam-scales:multi-load-cell

A sensor model that combines multiple load cell sensors (typically `configurable-scale` instances) into a single reading. It sums the weight across all cells and computes a force direction vector based on each cell's position.

## Configuration

```json
{
  "sensors": [
    {
      "sensor": "<string>",
      "distance_from_center_mm": <float>,
      "direction_degrees": <float>
    }
  ]
}
```

### Attributes

| Name | Type | Inclusion | Description |
|------|------|-----------|-------------|
| `sensors` | array | Required | List of load cell entries (see below) |

### Sensor Entry

| Name | Type | Inclusion | Description |
|------|------|-----------|-------------|
| `sensor` | string | Required | Name of an underlying sensor component (e.g. a `configurable-scale`) that provides `weight_kg` and `force_N` readings |
| `distance_from_center_mm` | float | Required | Distance from the center point to this load cell, in **millimeters** |
| `direction_degrees` | float | Required | Angular position of this load cell in **degrees** (0-360), measured counter-clockwise from the reference direction |

### Example Configuration

Three load cells arranged in a triangle, each 150mm from center:

```json
{
  "sensors": [
    {"sensor": "cell-1", "distance_from_center_mm": 150, "direction_degrees": 0},
    {"sensor": "cell-2", "distance_from_center_mm": 150, "direction_degrees": 120},
    {"sensor": "cell-3", "distance_from_center_mm": 150, "direction_degrees": 240}
  ]
}
```

## Readings

All readings are only present when the underlying sensors are calibrated (i.e. they provide `weight_kg`).

| Name | Type | Description |
|------|------|-------------|
| `total_weight_kg` | float | Sum of `weight_kg` from all underlying sensors, in **kg** |
| `total_force_N` | float | Sum of `force_N` from all underlying sensors, in **Newtons** |
| `force_direction_degrees` | float | Direction (0-360 **degrees**) where force is concentrated. Only present when force is unevenly distributed |
| `force_center_offset_ratio` | float | How off-center the force is (0 = perfectly centered). Ratio of the weighted position magnitude (in **mm**) to total weight (in **kg**). Only present when force is unevenly distributed |

### How force direction works

Each load cell has a known position defined by its distance and angle from center. When weight is unevenly distributed, cells closer to the load read higher values. The model computes a weighted vector sum using each cell's weight reading and position. The resulting vector's angle gives the force direction, and its magnitude (relative to total weight) indicates how far off-center the load is.

## DoCommand

### `tare`

Tares (zeros) all underlying sensors by forwarding a tare command to each one.

```json
{"tare": true}
```

Returns per-sensor tare results:

```json
{
  "sensor_0": {"offset": 1000.0},
  "sensor_1": {"offset": 1002.5},
  "sensor_2": {"offset": 998.3}
}
```
