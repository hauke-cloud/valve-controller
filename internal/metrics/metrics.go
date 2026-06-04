package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const ns = "valve_controller"

type Metrics struct {
	ValveState            *prometheus.GaugeVec
	ActionsTotal          *prometheus.CounterVec
	ActionDuration        *prometheus.HistogramVec
	ActionRetries         *prometheus.CounterVec
	SafetyClosesTotal     *prometheus.CounterVec
	MQTTMessagesReceived  *prometheus.CounterVec
	MQTTConnectionState   *prometheus.GaugeVec
	DailyIrrigationVolume *prometheus.GaugeVec
	LastOpenDuration      *prometheus.GaugeVec
	BatteryPercentage     *prometheus.GaugeVec
	LinkQuality           *prometheus.GaugeVec
}

func New(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		ValveState: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "valve_state",
			Help:      "Physical state of the valve: 1=open, 0=closed, -1=unknown.",
		}, []string{"name", "namespace"}),

		ActionsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "actions_total",
			Help:      "Total valve actions dispatched by type and result.",
		}, []string{"name", "namespace", "type", "result"}),

		ActionDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Name:      "action_duration_seconds",
			Help:      "Time in seconds from action request to confirmed fulfillment.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"name", "namespace", "type"}),

		ActionRetries: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "action_retries_total",
			Help:      "Total command retries due to missing device confirmation.",
		}, []string{"name", "namespace", "type"}),

		SafetyClosesTotal: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "safety_closes_total",
			Help:      "Total keep-closed safety pings sent.",
		}, []string{"name", "namespace"}),

		MQTTMessagesReceived: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "mqtt_messages_received_total",
			Help:      "Total MQTT messages received per bridge.",
		}, []string{"bridge"}),

		MQTTConnectionState: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "mqtt_connection_state",
			Help:      "MQTT broker connection state per bridge: 1=connected, 0=disconnected.",
		}, []string{"bridge"}),

		DailyIrrigationVolume: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "daily_irrigation_volume",
			Help:      "Daily irrigation volume as reported by the device.",
		}, []string{"name", "namespace"}),

		LastOpenDuration: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "last_open_duration_seconds",
			Help:      "Duration of the last open cycle in seconds as reported by the device.",
		}, []string{"name", "namespace"}),

		BatteryPercentage: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "battery_percentage",
			Help:      "Battery percentage as reported by the device.",
		}, []string{"name", "namespace"}),

		LinkQuality: f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "link_quality",
			Help:      "Zigbee link quality (0-255) as reported by the device.",
		}, []string{"name", "namespace"}),
	}
}

func ValveStateFloat(state string) float64 {
	switch state {
	case "open":
		return 1
	case "closed":
		return 0
	default:
		return -1
	}
}
