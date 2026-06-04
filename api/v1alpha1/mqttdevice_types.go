package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type MQTTDevice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MQTTDeviceSpec   `json:"spec"`
	Status MQTTDeviceStatus `json:"status,omitempty"`
}

type MQTTDeviceSpec struct {
	FriendlyName string        `json:"friendlyName"`
	BridgeRef    NamespacedRef `json:"bridgeRef"`
	TypeRef      *TypeRef      `json:"typeRef,omitempty"`
	IEEEAddr     string        `json:"ieeeAddr"`
	ShortAddr    string        `json:"shortAddr,omitempty"`
	Disabled     bool          `json:"disabled,omitempty"`
}

type NamespacedRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type TypeRef struct {
	APIGroup  string `json:"apiGroup"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type MQTTDeviceStatus struct {
	ModelID           string             `json:"modelId,omitempty"`
	Manufacturer      string             `json:"manufacturer,omitempty"`
	Reachable         bool               `json:"reachable,omitempty"`
	LinkQuality       int32              `json:"linkQuality,omitempty"`
	BatteryPercentage int32              `json:"batteryPercentage,omitempty"`
	LastSeenTime      *metav1.Time       `json:"lastSeenTime,omitempty"`
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type MQTTDeviceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MQTTDevice `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MQTTDevice{}, &MQTTDeviceList{})
}
