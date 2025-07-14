// Package regionmapper provides fallback cloud provider detection logic.
//
// DEPRECATED: This file contains legacy cloud provider detection logic that is being
// replaced by CloudInfo integration. These functions are maintained for backward
// compatibility and fallback scenarios only.
//
// New code should use the CloudInfo-based detection methods in cloudinfo_integration.go
// via RegionMapper struct methods like DetectCloudInfoFromCluster() and DetectCloudInfoFromNode().
package regionmapper

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// Known cloud providers
const (
	ProviderAWS   = "aws"
	ProviderGCP   = "gcp"
	ProviderAzure = "azure"
)

// cloudProviders defines metadata for detecting cloud providers
var cloudProviders = []CloudProviderInfo{
	{
		Name:             ProviderAWS,
		ProviderIDPrefix: "aws://",
		LabelSelectors:   []string{"node.kubernetes.io/instance-type"},
	},
	{
		Name:             ProviderGCP,
		ProviderIDPrefix: "gce://",
		LabelSelectors:   []string{"cloud.google.com/gke-nodepool"},
	},
	{
		Name:             ProviderAzure,
		ProviderIDPrefix: "azure://",
		LabelSelectors:   []string{"kubernetes.azure.com/cluster"},
	},
}

// DetectCloudProviderAndRegion attempts to determine the cloud provider and region from node metadata
//
// DEPRECATED: This function is deprecated in favor of CloudInfo-based detection.
// Use RegionMapper.detectProviderAndRegionWithFallback() instead, which tries CloudInfo first
// and falls back to this function only when CloudInfo fails.
//
// This function is maintained for backward compatibility and fallback scenarios.
func DetectCloudProviderAndRegion(node *v1.Node) (provider string, region string, ok bool) {
	if node == nil {
		return "", "", false
	}

	// Try to detect provider first
	provider = DetectCloudProvider(node)
	if provider == "" {
		return "", "", false
	}

	// Try standard Kubernetes topology labels first (most reliable)
	region, regionFound := node.Labels["topology.kubernetes.io/region"]
	if regionFound && region != "" {
		klog.V(3).InfoS("Detected region from standard topology label",
			"node", node.Name,
			"provider", provider,
			"region", region)
		return provider, region, true
	}

	// If standard label not found, try provider-specific detection
	switch provider {
	case ProviderAWS:
		region, regionFound = detectAWSRegion(node)
	case ProviderGCP:
		region, regionFound = detectGCPRegion(node)
	case ProviderAzure:
		region, regionFound = detectAzureRegion(node)
	}

	if regionFound && region != "" {
		klog.V(3).InfoS("Detected region from provider-specific metadata",
			"node", node.Name,
			"provider", provider,
			"region", region)
		return provider, region, true
	}

	klog.V(3).InfoS("Could not detect cloud region",
		"node", node.Name,
		"provider", provider)
	return provider, "", false
}

// DetectCloudProvider determines the cloud provider from node labels and provider ID
//
// DEPRECATED: This function is deprecated in favor of CloudInfo-based detection.
// Use cloudinfo.ParseProviderID() for provider detection from ProviderID.
//
// This function is maintained for backward compatibility and fallback scenarios.
func DetectCloudProvider(node *v1.Node) string {
	if node == nil {
		return ""
	}

	// First check providerID as it's the most reliable
	if node.Spec.ProviderID != "" {
		for _, provider := range cloudProviders {
			if strings.HasPrefix(node.Spec.ProviderID, provider.ProviderIDPrefix) {
				klog.V(3).InfoS("Detected cloud provider from providerID",
					"node", node.Name,
					"provider", provider.Name,
					"providerID", node.Spec.ProviderID)
				return provider.Name
			}
		}
	}

	// If providerID doesn't match, try labels
	for _, provider := range cloudProviders {
		for _, labelKey := range provider.LabelSelectors {
			if _, exists := node.Labels[labelKey]; exists {
				klog.V(3).InfoS("Detected cloud provider from labels",
					"node", node.Name,
					"provider", provider.Name,
					"label", labelKey)
				return provider.Name
			}
		}
	}

	return ""
}

// detectAWSRegion attempts to determine the AWS region from node metadata
//
// DEPRECATED: Use CloudInfo-based detection instead.
func detectAWSRegion(node *v1.Node) (string, bool) {
	// Try to extract from providerID
	// Format: aws://region/instance-id
	if node.Spec.ProviderID != "" && strings.HasPrefix(node.Spec.ProviderID, "aws://") {
		parts := strings.Split(strings.TrimPrefix(node.Spec.ProviderID, "aws://"), "/")
		if len(parts) >= 2 && parts[0] != "" {
			return parts[0], true
		}
	}

	// Try to extract from labels
	// Some AWS integrations add this label
	if region, exists := node.Labels["failure-domain.beta.kubernetes.io/region"]; exists && region != "" {
		return region, true
	}

	// Try to extract from the zone, which often contains the region
	if zone, exists := node.Labels["topology.kubernetes.io/zone"]; exists && zone != "" {
		// AWS zones have format like us-west-2a, us-east-1b, etc.
		// We need to remove the last character to get the region
		if len(zone) > 1 && strings.Count(zone, "-") >= 2 {
			return zone[:len(zone)-1], true
		}
	}

	return "", false
}

// detectGCPRegion attempts to determine the GCP region from node metadata
//
// DEPRECATED: Use CloudInfo-based detection instead.
func detectGCPRegion(node *v1.Node) (string, bool) {
	// Try to extract from providerID
	// Format: gce://project/zone/instance
	if node.Spec.ProviderID != "" && strings.HasPrefix(node.Spec.ProviderID, "gce://") {
		parts := strings.Split(strings.TrimPrefix(node.Spec.ProviderID, "gce://"), "/")
		if len(parts) >= 3 && parts[1] != "" {
			// GCP zones have format like us-central1-a
			// We need to remove the last part to get the region
			zone := parts[1]
			lastDash := strings.LastIndex(zone, "-")
			if lastDash > 0 {
				return zone[:lastDash], true
			}
			return zone, true
		}
	}

	// Try to extract from the zone
	if zone, exists := node.Labels["topology.kubernetes.io/zone"]; exists && zone != "" {
		// GCP zones have format like us-central1-a
		lastDash := strings.LastIndex(zone, "-")
		if lastDash > 0 {
			return zone[:lastDash], true
		}
		return zone, true
	}

	return "", false
}

// detectAzureRegion attempts to determine the Azure region from node metadata
//
// DEPRECATED: Use CloudInfo-based detection instead.
func detectAzureRegion(node *v1.Node) (string, bool) {
	// Try to extract from providerID
	// Format: azure:///subscriptions/sub-id/resourceGroups/MC_resource-group_cluster-name_location/providers/Microsoft.Compute/virtualMachines/node-name
	if node.Spec.ProviderID != "" && strings.HasPrefix(node.Spec.ProviderID, "azure://") {
		// This is complex to parse directly, fall back to other methods
	}

	// Check for azure-specific region label
	if region, exists := node.Labels["failure-domain.beta.kubernetes.io/region"]; exists && region != "" {
		return region, true
	}

	// Recent AKS also has this label
	if location, exists := node.Labels["kubernetes.azure.com/location"]; exists && location != "" {
		return location, true
	}

	return "", false
}
