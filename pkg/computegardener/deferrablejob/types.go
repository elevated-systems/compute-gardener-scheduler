package deferrablejob

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeferrableJobSpec defines the desired state of DeferrableJob
type DeferrableJobSpec struct {
	Priority           int                `json:"priority"`
	Timeout            int                `json:"timeout"`
	Schedule           string             `json:"schedule"`
	PricingConstraints PricingConstraints `json:"pricingConstraints"`
	CarbonConstraints  CarbonConstraints  `json:"carbonConstraints"`
}

// PricingConstraints holds pricing-related constraints for a deferrable job
type PricingConstraints struct {
	MaxPrice         float64  `json:"maxPrice"`
	PreferredRegions []string `json:"preferredRegions"`
}

// CarbonConstraints holds carbon-related constraints for a deferrable job
type CarbonConstraints struct {
	MaxCarbonEmission  float64  `json:"maxCarbonEmission"`
	PreferredProviders []string `json:"preferredProviders"`
}

// DeferrableJobStatus defines the observed state of DeferrableJob
type DeferrableJobStatus struct {
	LastScheduledTime metav1.Time `json:"lastScheduledTime,omitempty"`
}

// +kubebuilder:object:root=true

// DeferrableJob is the Schema for the deferrablejobs API
type DeferrableJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeferrableJobSpec   `json:"spec,omitempty"`
	Status DeferrableJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeferrableJobList contains a list of DeferrableJob
type DeferrableJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeferrableJob `json:"items"`
}
