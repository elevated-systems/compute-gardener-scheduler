package almanac

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

// NodeInfo contains extracted cloud provider information from a node
type NodeInfo struct {
	Provider     string
	Region       string
	Zone         string
	InstanceType string
}

// ExtractNodeInfo extracts cloud provider information from node labels
// Returns NodeInfo with available fields populated, some may be empty
func ExtractNodeInfo(node *v1.Node) NodeInfo {
	if node == nil {
		return NodeInfo{}
	}

	info := NodeInfo{}

	// Extract region from standard K8s labels
	if region, ok := node.Labels[common.LabelTopologyRegion]; ok && region != "" {
		info.Region = region
	}

	// Extract zone from standard K8s labels
	if zone, ok := node.Labels[common.LabelTopologyZone]; ok && zone != "" {
		info.Zone = zone
	}

	// Extract instance type (try standard label first, then legacy)
	if instanceType, ok := node.Labels[common.LabelNodeInstanceType]; ok && instanceType != "" {
		info.InstanceType = instanceType
	} else if instanceType, ok := node.Labels[common.LabelBetaInstanceType]; ok && instanceType != "" {
		info.InstanceType = instanceType
	}

	// Infer provider from instance type prefix or provider ID
	info.Provider = inferProvider(node, info.InstanceType)

	return info
}

// inferProvider attempts to determine the cloud provider from various signals
func inferProvider(node *v1.Node, instanceType string) string {
	// Check providerID first (format: provider://region/instance-id)
	if node.Spec.ProviderID != "" {
		parts := strings.SplitN(node.Spec.ProviderID, "://", 2)
		if len(parts) == 2 {
			provider := parts[0]
			// Normalize provider names
			switch provider {
			case "aws":
				return "aws"
			case "gce", "gcp":
				return "gcp"
			case "azure":
				return "azure"
			}
			return provider
		}
	}

	// Try to infer from instance type patterns
	if instanceType != "" {
		// AWS instances: m5.xlarge, c6i.2xlarge, etc.
		if strings.Contains(instanceType, ".") {
			parts := strings.Split(instanceType, ".")
			if len(parts) >= 2 {
				// Check for AWS-style naming
				family := parts[0]
				if len(family) >= 2 && (family[0] >= 'a' && family[0] <= 'z') {
					// Simple heuristic: AWS uses letter+number+letter patterns like m5, c6i, etc.
					return "aws"
				}
			}
		}

		// GCP instances: n1-standard-1, n2-highmem-4, etc.
		if strings.Contains(instanceType, "-") {
			parts := strings.Split(instanceType, "-")
			if len(parts) >= 2 {
				family := parts[0]
				// GCP uses patterns like n1, n2, e2, etc.
				if strings.HasPrefix(family, "n") || strings.HasPrefix(family, "e") || strings.HasPrefix(family, "c2") {
					return "gcp"
				}
			}
		}

		// Azure instances: Standard_D2s_v3, Standard_F4s_v2, etc.
		if strings.HasPrefix(instanceType, "Standard_") {
			return "azure"
		}
	}

	// Check for cloud-specific labels
	for label := range node.Labels {
		if strings.Contains(label, "eks.amazonaws.com") {
			return "aws"
		}
		if strings.Contains(label, "cloud.google.com") {
			return "gcp"
		}
		if strings.Contains(label, "kubernetes.azure.com") {
			return "azure"
		}
	}

	// Unable to determine provider
	return ""
}

// String returns a string representation of NodeInfo
func (ni NodeInfo) String() string {
	return fmt.Sprintf("provider=%s region=%s zone=%s instanceType=%s",
		ni.Provider, ni.Region, ni.Zone, ni.InstanceType)
}

// IsComplete returns true if we have enough information for scoring
// (at minimum, we need either zone OR (provider + region))
func (ni NodeInfo) IsComplete() bool {
	hasZone := ni.Zone != ""
	hasProviderAndRegion := ni.Provider != "" && ni.Region != ""
	return hasZone || hasProviderAndRegion
}

// LogMissing logs any missing fields at V(3) level for debugging
func (ni NodeInfo) LogMissing(nodeName string) {
	missing := []string{}
	if ni.Provider == "" {
		missing = append(missing, "provider")
	}
	if ni.Region == "" {
		missing = append(missing, "region")
	}
	if ni.Zone == "" {
		missing = append(missing, "zone")
	}
	if ni.InstanceType == "" {
		missing = append(missing, "instanceType")
	}

	if len(missing) > 0 {
		klog.V(3).InfoS("Incomplete node information for scoring",
			"node", nodeName,
			"missing", strings.Join(missing, ", "),
			"extracted", ni.String())
	}
}
