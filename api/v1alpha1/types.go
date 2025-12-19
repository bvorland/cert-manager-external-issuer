package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExternalIssuerSpec defines the desired state of ExternalIssuer
type ExternalIssuerSpec struct {
	// URL is the base URL of the CA API (used when configMapRef is not set)
	// This is primarily for testing with the built-in Mock CA
	// +optional
	URL string `json:"url,omitempty"`

	// ConfigMapRef references a ConfigMap containing PKI API configuration
	// This allows dynamic configuration without rebuilding the controller
	// +optional
	ConfigMapRef *ConfigMapReference `json:"configMapRef,omitempty"`

	// AuthSecretName is the name of a Secret containing authentication credentials
	// The secret should contain a key named 'token', 'api-key', or 'password'
	// +optional
	AuthSecretName string `json:"authSecretName,omitempty"`

	// SignerType specifies which signer to use: "mockca" or "pki"
	// - "mockca": Use the built-in Mock CA (for testing/development)
	// - "pki": Use the external PKI API configured in configMapRef
	// Default is "mockca" for backward compatibility
	// +optional
	// +kubebuilder:validation:Enum=mockca;pki
	// +kubebuilder:default=mockca
	SignerType string `json:"signerType,omitempty"`
}

// ConfigMapReference references a ConfigMap in a namespace
type ConfigMapReference struct {
	// Name is the name of the ConfigMap
	Name string `json:"name"`

	// Namespace is the namespace of the ConfigMap
	// For ExternalIssuer: defaults to the issuer's namespace
	// For ExternalClusterIssuer: defaults to "external-issuer-system"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Key is the key in the ConfigMap data containing the JSON configuration
	// Defaults to "pki-config.json"
	// +optional
	// +kubebuilder:default="pki-config.json"
	Key string `json:"key,omitempty"`
}

// ExternalIssuerStatus defines the observed state of ExternalIssuer
type ExternalIssuerStatus struct {
	// Conditions represent the latest observed conditions of the issuer
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ExternalIssuer is the Schema for the externalissuers API
// It defines a namespaced issuer that can issue certificates within its namespace
type ExternalIssuer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalIssuerSpec   `json:"spec,omitempty"`
	Status ExternalIssuerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExternalIssuerList contains a list of ExternalIssuer
type ExternalIssuerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalIssuer `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ExternalClusterIssuer is the Schema for the externalclusterissuers API
// It defines a cluster-wide issuer that can issue certificates across all namespaces
type ExternalClusterIssuer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalIssuerSpec   `json:"spec,omitempty"`
	Status ExternalIssuerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExternalClusterIssuerList contains a list of ExternalClusterIssuer
type ExternalClusterIssuerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalClusterIssuer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExternalIssuer{}, &ExternalIssuerList{})
	SchemeBuilder.Register(&ExternalClusterIssuer{}, &ExternalClusterIssuerList{})
}
