package device

import "time"

// ValveState mirrors the CRD enum for internal use without importing api/v1alpha1.
type ValveState string

const (
	ValveStateOpen    ValveState = "open"
	ValveStateClosed  ValveState = "closed"
	ValveStateUnknown ValveState = "unknown"
)

// ValveConfig holds tuning parameters derived from MQTTValveSpec.
type ValveConfig struct {
	Disabled           bool
	RetryCount         int
	CommandTimeout     time.Duration
	KeepClosedInterval time.Duration
	MaxOpenDuration    time.Duration // 0 = no limit
	CloseRepeatCount   int           // times to send each close command
}

// ValveInfo is the fully-resolved runtime representation of a managed valve,
// joining MQTTValve + MQTTDevice + MQTTBridge into a single struct.
type ValveInfo struct {
	// from MQTTValve
	Name      string
	Namespace string
	Config    ValveConfig

	// from MQTTDevice (discovered via typeRef → MQTTValve)
	FriendlyName string // Tasmota friendly name — mutable, renamed by the device controller
	ShortAddr    string // Zigbee short address — stable, preferred for addressing the device

	// from MQTTBridge (resolved via MQTTDevice.bridgeRef)
	BridgeName string // spec.bridgeName — used to compose MQTT topics
	BridgeHost string
	BridgePort int32

	// live sensor fields (updated by scheduler on MQTT events)
	State                 ValveState
	LinkQuality           int32
	BatteryPercentage     int32
	Reachable             bool
	DailyIrrigationVolume float64
	LastValveOpenDuration int64 // milliseconds
	CurrentActionID       string
	LastOpenTime          *time.Time
	LastCloseTime         *time.Time
}

// Key returns the unique string identifier used in maps and log fields.
func (v *ValveInfo) Key() string {
	return v.Namespace + "/" + v.Name
}

// CommandTarget returns the identifier used to address the device on the
// bridge. The short address is preferred because it survives friendly-name
// renames; Tasmota accepts either form in ZbSend and ZbStatus3. Addressing by
// friendly name means every command and every confirmation depends on a value
// that another controller can change underneath us at any moment.
func (v *ValveInfo) CommandTarget() string {
	if v.ShortAddr != "" {
		return v.ShortAddr
	}
	return v.FriendlyName
}
