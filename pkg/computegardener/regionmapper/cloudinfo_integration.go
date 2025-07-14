// Package regionmapper provides CloudInfo-based cloud provider detection.
//
// This file contains the primary cloud provider and region detection logic using
// the CloudInfo package. This replaces the legacy detection logic in fallback_detector.go.
//
// CloudInfo provides more robust detection using Kubernetes node labels and
// Instance Metadata Service (IMDS) fallback when available.
package regionmapper

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/carbon-aware/cloudinfo/pkg/cloudinfo"
)

// DetectCloudInfoFromCluster uses CloudInfo to detect provider and region from the entire cluster
func (m *RegionMapper) DetectCloudInfoFromCluster(ctx context.Context) (string, string, bool) {
	if m.client == nil {
		klog.V(2).InfoS("No Kubernetes client available for CloudInfo detection")
		return "", "", false
	}

	// Use CloudInfo for detection with node labels as primary method
	opts := cloudinfo.Options{
		UseNodeLabels: true,
		UseIMDS:       false, // Don't use IMDS in cluster context
	}

	cloudInfo, err := cloudinfo.DetectCloudInfo(ctx, m.client, opts)
	if err != nil {
		klog.V(2).InfoS("CloudInfo detection failed", "error", err)
		return "", "", false
	}

	klog.V(3).InfoS("CloudInfo detected provider and region",
		"provider", cloudInfo.Provider,
		"region", cloudInfo.Region,
		"source", cloudInfo.Source)

	return cloudInfo.Provider, cloudInfo.Region, true
}

// detectCloudInfoFromNode uses CloudInfo patterns to detect provider and region for a specific node
func (m *RegionMapper) detectCloudInfoFromNode(node *v1.Node) (string, string, bool) {
	if node == nil {
		return "", "", false
	}

	// Check standard region label first
	if region, exists := node.Labels["topology.kubernetes.io/region"]; exists && region != "" {
		// Parse provider from ProviderID using CloudInfo logic
		if node.Spec.ProviderID != "" {
			provider, err := cloudinfo.ParseProviderID(node.Spec.ProviderID)
			if err == nil {
				klog.V(3).InfoS("CloudInfo detected provider and region from node",
					"node", node.Name,
					"provider", provider,
					"region", region)
				return provider, region, true
			}
		}
	}

	return "", "", false
}

// detectProviderAndRegionWithFallback attempts CloudInfo detection first, then falls back to legacy detection
//
// This function implements a graceful migration strategy:
// 1. Primary: Try CloudInfo-based detection (more robust, externally maintained)
// 2. Fallback: Use legacy detection from fallback_detector.go (backward compatibility)
//
// The fallback ensures no breaking changes during the CloudInfo migration.
func (m *RegionMapper) detectProviderAndRegionWithFallback(node *v1.Node) (string, string, bool) {
	// Try CloudInfo detection first
	provider, region, ok := m.detectCloudInfoFromNode(node)
	if ok {
		return provider, region, true
	}

	// Fallback to existing detection logic in fallback_detector.go
	return DetectCloudProviderAndRegion(node)
}
