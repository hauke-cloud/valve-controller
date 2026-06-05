package v1alpha1

// Type aliases re-exported from mqtt-device-controller so the rest of the
// codebase can keep a single "iotv1alpha1" import for all CRD types.
// The alias form (=) means these are the exact same types — no wrapping,
// no conversion, scheme registration from the upstream package applies.

import iotdevv1alpha1 "github.com/hauke-cloud/mqtt-device-controller/api/v1alpha1"

type (
	MQTTBridge           = iotdevv1alpha1.MQTTBridge
	MQTTBridgeList       = iotdevv1alpha1.MQTTBridgeList
	MQTTBridgeSpec       = iotdevv1alpha1.MQTTBridgeSpec
	MQTTBridgeStatus     = iotdevv1alpha1.MQTTBridgeStatus
	BridgeCredentialsRef = iotdevv1alpha1.CredentialsSecretRef

	MQTTDevice       = iotdevv1alpha1.MQTTDevice
	MQTTDeviceList   = iotdevv1alpha1.MQTTDeviceList
	MQTTDeviceSpec   = iotdevv1alpha1.MQTTDeviceSpec
	MQTTDeviceStatus = iotdevv1alpha1.MQTTDeviceStatus

	NamespacedRef = iotdevv1alpha1.ObjectRef
	TypeRef       = iotdevv1alpha1.TypeRef

	TopicConfig = iotdevv1alpha1.TopicConfig
	TopicType   = iotdevv1alpha1.TopicType
)
