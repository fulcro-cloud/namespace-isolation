package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto copies the receiver into out
func (in *NamespaceQuota) DeepCopyInto(out *NamespaceQuota) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy
func (in *NamespaceQuota) DeepCopy() *NamespaceQuota {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuota)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject creates a deep copy as runtime.Object
func (in *NamespaceQuota) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// DeepCopyInto copies spec
func (in *NamespaceQuotaSpec) DeepCopyInto(out *NamespaceQuotaSpec) {
	*out = *in
	if in.Enabled != nil {
		out.Enabled = new(bool)
		*out.Enabled = *in.Enabled
	}
}

// DeepCopy creates a deep copy of spec
func (in *NamespaceQuotaSpec) DeepCopy() *NamespaceQuotaSpec {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuotaSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies status
func (in *NamespaceQuotaStatus) DeepCopyInto(out *NamespaceQuotaStatus) {
	*out = *in
	if in.LastUpdated != nil {
		out.LastUpdated = in.LastUpdated.DeepCopy()
	}
}

// DeepCopy creates a deep copy of status
func (in *NamespaceQuotaStatus) DeepCopy() *NamespaceQuotaStatus {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuotaStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies list
func (in *NamespaceQuotaList) DeepCopyInto(out *NamespaceQuotaList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]NamespaceQuota, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy creates a deep copy of list
func (in *NamespaceQuotaList) DeepCopy() *NamespaceQuotaList {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuotaList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject creates a deep copy as runtime.Object
func (in *NamespaceQuotaList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
