// Package v1alpha1 contains the iot.hauke.cloud/v1alpha1 API types.
//
// The markers below are what let controller-gen resolve the API group; without
// them `make manifests` emits a group-less "_.yaml" instead of regenerating the
// CRD, which is how spec fields end up in the Go types but never in the
// deployed schema.
//
// +kubebuilder:object:generate=true
// +groupName=iot.hauke.cloud
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "iot.hauke.cloud", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)
