package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NamespaceQuota defines resource limits for a Kubernetes namespace
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NamespaceQuota struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespaceQuotaSpec   `json:"spec"`
	Status NamespaceQuotaStatus `json:"status,omitempty"`
}

// NamespaceQuotaSpec defines the desired state
type NamespaceQuotaSpec struct {
	// Namespace is the target Kubernetes namespace
	Namespace string `json:"namespace"`

	// CPU limit in cores (e.g., "4" for 4 cores)
	CPU string `json:"cpu,omitempty"`

	// Memory limit (e.g., "8Gi", "512Mi")
	Memory string `json:"memory,omitempty"`

	// Enabled controls if quota is enforced
	Enabled *bool `json:"enabled,omitempty"`
}

// NamespaceQuotaStatus defines the observed state
type NamespaceQuotaStatus struct {
	// Ready indicates if the cgroup is configured
	Ready bool `json:"ready,omitempty"`

	// Message provides additional details
	Message string `json:"message,omitempty"`

	// LastUpdated timestamp
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// IsEnabled returns true if the quota is enabled (defaults to true)
func (q *NamespaceQuota) IsEnabled() bool {
	if q.Spec.Enabled == nil {
		return true
	}
	return *q.Spec.Enabled
}

// NamespaceQuotaList is a list of NamespaceQuota
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NamespaceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NamespaceQuota `json:"items"`
}
