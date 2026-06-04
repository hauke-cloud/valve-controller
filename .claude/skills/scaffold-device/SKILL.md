---
name: scaffold-device
description: Scaffold all boilerplate for a new Zigbee device type: Go domain type, CRD type + markers, REST handler registration, TimescaleDB metric constants, and MQTT key mapping. Run when the user wants to add support for a new class of physical Zigbee device (e.g. a temperature sensor, a water valve, a motion detector).
---

# scaffold-device

Do NOT write any code until all 4 steps are complete.

---

## Step 1 — Gather device facts

Ask (or infer from context) and write down:

```
Device class name:    e.g. WaterValve, TempHumiditySensor, MotionSensor
Tasmota JSON keys:    e.g. StateValve, Temperature, Humidity, LinkQuality, BatteryPercentage
Metrics to store:     e.g. valve_state (dimensionless), temperature (celsius), battery (percent)
Capabilities:         which of CapabilityOnOff | CapabilityValve | CapabilityTemperature | CapabilityHumidity | CapabilityBattery
Commands supported:   e.g. Power=0/1, Setpoint=22.5
CRD short name:       e.g. wv, ths, ms  (max 4 chars, lowercase)
```

Do not proceed until every field above is filled.

---

## Step 2 — Files to create / edit

| File | Action |
|------|--------|
| `internal/device/<classname_lower>.go` | Create: Go struct, capability set, Tasmota key map, command builder |
| `internal/device/registry.go` | Edit: register the new device model |
| `api/v1alpha1/<classname_lower>_types.go` | Create: CRD types with kubebuilder markers |
| `api/v1alpha1/groupversion_info.go` | Edit: add new Kind to `SchemeBuilder.Register` call |
| `internal/api/handlers.go` | Edit: no new handler needed — generic device endpoints handle all types |
| `internal/db/metrics.go` | Edit: add metric name constants for new metrics |
| `config/crd/bases/` | Regenerate after writing types (see Step 4) |

---

## Step 3 — Write the code

### 3a. Device domain type (`internal/device/<name>.go`)

```go
package device

// <ClassName> implements DeviceModel for <description>.
type <ClassName>Model struct{}

var _ DeviceModel = (*<ClassName>Model)(nil)

func (m *<ClassName>Model) Capabilities() []Capability {
    return []Capability{Capability<X>, Capability<Y>}
}

// TasmotaKeyMap maps Tasmota JSON keys to metric names.
func (m *<ClassName>Model) TasmotaKeyMap() map[string]string {
    return map[string]string{
        "<TasmotaKey1>": Metric<Name1>,
        "<TasmotaKey2>": Metric<Name2>,
    }
}

// BuildCommand converts an API command request to a ZbSend payload.
func (m *<ClassName>Model) BuildCommand(cmd CommandRequest) (map[string]any, error) {
    switch cmd.Action {
    case "open":
        return map[string]any{"Power": 1}, nil
    case "close":
        return map[string]any{"Power": 0}, nil
    default:
        return nil, fmt.Errorf("unsupported action %q: %w", cmd.Action, ErrUnsupportedCommand)
    }
}
```

### 3b. Register in `internal/device/registry.go`

```go
func init() {
    Register("<DeviceType enum value>", &<ClassName>Model{})
}
```

### 3c. CRD types (`api/v1alpha1/<name>_types.go`)

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=<shortname>
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type <ClassName> struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   <ClassName>Spec   `json:"spec"`
    Status <ClassName>Status `json:"status,omitempty"`
}

type <ClassName>Spec struct {
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    DeviceName string `json:"deviceName"`

    // +kubebuilder:validation:Required
    BridgeTopic string `json:"bridgeTopic"`

    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=3600
    PollIntervalSeconds int32 `json:"pollIntervalSeconds,omitempty"`

    // <device-specific fields here>
}

type <ClassName>Status struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // Last observed state fields…
}

// +kubebuilder:object:root=true
type <ClassName>List struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []<ClassName> `json:"items"`
}
```

### 3d. Metric constants (`internal/db/metrics.go`)

```go
const (
    Metric<Name1> = "<metric_name_1>"   // unit: <unit>
    Metric<Name2> = "<metric_name_2>"   // unit: <unit>
)
```

---

## Step 4 — Regenerate CRD manifests and verify

```bash
# Regenerate DeepCopy methods
controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/...

# Regenerate CRD YAML
controller-gen crd:trivialVersions=false \
  paths=./api/... \
  output:crd:artifacts:config=config/crd/bases

# Build check
go build ./...

# Tests
go test ./internal/device/... -race
go test ./api/... -race
```

All must pass before declaring done.

---

## Verification checklist

- [ ] Device facts filled in (Step 1)
- [ ] `TasmotaKeyMap` covers every key listed in Step 1
- [ ] `BuildCommand` returns `ErrUnsupportedCommand` for unknown actions
- [ ] New Kind registered in `SchemeBuilder.Register`
- [ ] CRD YAML regenerated and committed alongside Go types
- [ ] Metric constants added for each new metric
- [ ] `go build ./... && go test ./... -race` exits 0
