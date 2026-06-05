package v1alpha1

// Hand-written DeepCopyInto for non-root types that controller-gen calls but
// does not generate bodies for (controller-gen v0.17 emits call sites in root
// types but skips method bodies for Spec/Status types in multi-type packages).

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (in *MQTTValveStatus) DeepCopyInto(out *MQTTValveStatus) {
	*out = *in
	if in.LastOpenTime != nil {
		in, out := &in.LastOpenTime, &out.LastOpenTime
		*out = (*in).DeepCopy()
	}
	if in.LastCloseTime != nil {
		in, out := &in.LastCloseTime, &out.LastCloseTime
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}
