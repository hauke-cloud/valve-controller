package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=mv
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.valveState"
// +kubebuilder:printcolumn:name="Action",type="string",JSONPath=".status.currentActionID"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MQTTValve struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MQTTValveSpec   `json:"spec"`
	Status MQTTValveStatus `json:"status,omitempty"`
}

type MQTTValveSpec struct {
	// Disabled prevents the valve from accepting open commands.
	// +kubebuilder:default=false
	Disabled bool `json:"disabled,omitempty"`

	// RetryCount is the number of send attempts before an action is marked failed.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	RetryCount int32 `json:"retryCount,omitempty"`

	// CommandTimeoutSeconds is the per-attempt timeout waiting for device confirmation.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=5
	// +kubebuilder:validation:Maximum=300
	CommandTimeoutSeconds int32 `json:"commandTimeoutSeconds,omitempty"`

	// KeepClosedIntervalSeconds controls how often the controller re-sends the close command as a safety net.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=3600
	KeepClosedIntervalSeconds int32 `json:"keepClosedIntervalSeconds,omitempty"`

	// MaxOpenDurationSeconds is a hard safety limit on how long the valve may remain open.
	// 0 disables the limit (not recommended for garden irrigation).
	// +kubebuilder:default=3600
	// +kubebuilder:validation:Minimum=0
	MaxOpenDurationSeconds int32 `json:"maxOpenDurationSeconds,omitempty"`
}

// ValveState is the confirmed physical state of the valve.
// +kubebuilder:validation:Enum=open;closed;unknown
type ValveState string

const (
	ValveStateOpen    ValveState = "open"
	ValveStateClosed  ValveState = "closed"
	ValveStateUnknown ValveState = "unknown"
)

type MQTTValveStatus struct {
	// ValveState is the last confirmed physical state from the device.
	// +kubebuilder:validation:Enum=open;closed;unknown
	ValveState ValveState `json:"valveState,omitempty"`

	// LastOpenTime is when the valve was last confirmed open.
	LastOpenTime *metav1.Time `json:"lastOpenTime,omitempty"`

	// LastCloseTime is when the valve was last confirmed closed.
	LastCloseTime *metav1.Time `json:"lastCloseTime,omitempty"`

	// CurrentActionID is the ID of the in-progress action; empty when idle.
	CurrentActionID string `json:"currentActionID,omitempty"`

	// DailyIrrigationVolume from the last device sensor report.
	DailyIrrigationVolume float64 `json:"dailyIrrigationVolume,omitempty"`

	// LastValveOpenDuration from the last device report, in milliseconds.
	LastValveOpenDuration int64 `json:"lastValveOpenDuration,omitempty"`

	// Conditions holds the Ready and Reachable conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type MQTTValveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MQTTValve `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MQTTValve{}, &MQTTValveList{})
}
