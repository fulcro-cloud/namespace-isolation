package agent

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var NamespaceQuotaGVR = schema.GroupVersionResource{
	Group:    "brasa.cloud",
	Version:  "v1alpha1",
	Resource: "namespacequotas",
}

type NamespaceQuotaSpec struct {
	Namespace string
	CPU       string
	Memory    string
	Enabled   bool
}

func ParseNamespaceQuota(obj *unstructured.Unstructured) (*NamespaceQuotaSpec, error) {
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("spec not found in NamespaceQuota")
	}

	namespace, _, _ := unstructured.NestedString(spec, "namespace")
	if namespace == "" {
		return nil, fmt.Errorf("namespace field is required")
	}

	cpu, _, _ := unstructured.NestedString(spec, "cpu")
	memory, _, _ := unstructured.NestedString(spec, "memory")

	enabled := true
	if enabledVal, found, _ := unstructured.NestedBool(spec, "enabled"); found {
		enabled = enabledVal
	}

	return &NamespaceQuotaSpec{
		Namespace: namespace,
		CPU:       cpu,
		Memory:    memory,
		Enabled:   enabled,
	}, nil
}
