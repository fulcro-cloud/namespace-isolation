package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

func (in *NamespaceQuota) DeepCopyInto(out *NamespaceQuota) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *NamespaceQuota) DeepCopy() *NamespaceQuota {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuota)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQuota) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *NamespaceQuotaSpec) DeepCopyInto(out *NamespaceQuotaSpec) {
	*out = *in
	if in.Enabled != nil {
		out.Enabled = new(bool)
		*out.Enabled = *in.Enabled
	}
}

func (in *NamespaceQuotaSpec) DeepCopy() *NamespaceQuotaSpec {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuotaSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQuotaStatus) DeepCopyInto(out *NamespaceQuotaStatus) {
	*out = *in
	if in.LastUpdated != nil {
		out.LastUpdated = in.LastUpdated.DeepCopy()
	}
}

func (in *NamespaceQuotaStatus) DeepCopy() *NamespaceQuotaStatus {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuotaStatus)
	in.DeepCopyInto(out)
	return out
}

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

func (in *NamespaceQuotaList) DeepCopy() *NamespaceQuotaList {
	if in == nil {
		return nil
	}
	out := new(NamespaceQuotaList)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQuotaList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
