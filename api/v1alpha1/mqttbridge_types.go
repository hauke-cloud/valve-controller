package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type MQTTBridge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MQTTBridgeSpec   `json:"spec"`
	Status MQTTBridgeStatus `json:"status,omitempty"`
}

type MQTTBridgeSpec struct {
	BridgeName                 string                `json:"bridgeName"`
	Host                       string                `json:"host"`
	Port                       int32                 `json:"port,omitempty"`
	CredentialsSecretRef       *BridgeCredentialsRef `json:"credentialsSecretRef,omitempty"`
	MaxReconnectBackoffSeconds int32                 `json:"maxReconnectBackoffSeconds,omitempty"`
}

type BridgeCredentialsRef struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	UsernameKey string `json:"usernameKey,omitempty"`
	PasswordKey string `json:"passwordKey,omitempty"`
}

type MQTTBridgeStatus struct {
	ConnectionState      string             `json:"connectionState,omitempty"`
	LastConnectedTime    *metav1.Time       `json:"lastConnectedTime,omitempty"`
	LastDisconnectedTime *metav1.Time       `json:"lastDisconnectedTime,omitempty"`
	Conditions           []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
type MQTTBridgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MQTTBridge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MQTTBridge{}, &MQTTBridgeList{})
}
