package almanac

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
)

func TestExtractNodeInfo(t *testing.T) {
	tests := []struct {
		name             string
		node             *v1.Node
		wantProvider     string
		wantRegion       string
		wantZone         string
		wantInstanceType string
	}{
		{
			name: "AWS EKS node with all labels",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "aws:///us-west-2a/i-1234567890abcdef0",
				},
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelTopologyRegion:   "us-west-2",
						common.LabelTopologyZone:     "us-west-2a",
						common.LabelNodeInstanceType: "m5.xlarge",
					},
				},
			},
			wantProvider:     "aws",
			wantRegion:       "us-west-2",
			wantZone:         "us-west-2a",
			wantInstanceType: "m5.xlarge",
		},
		{
			name: "GCP GKE node",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "gce://my-project/us-central1-a/instance-name",
				},
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelTopologyRegion:   "us-central1",
						common.LabelTopologyZone:     "us-central1-a",
						common.LabelNodeInstanceType: "n1-standard-4",
					},
				},
			},
			wantProvider:     "gcp",
			wantRegion:       "us-central1",
			wantZone:         "us-central1-a",
			wantInstanceType: "n1-standard-4",
		},
		{
			name: "Azure AKS node",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "azure:///subscriptions/sub-id/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				},
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelTopologyRegion:   "eastus",
						common.LabelTopologyZone:     "eastus-1",
						common.LabelNodeInstanceType: "Standard_D4s_v3",
					},
				},
			},
			wantProvider:     "azure",
			wantRegion:       "eastus",
			wantZone:         "eastus-1",
			wantInstanceType: "Standard_D4s_v3",
		},
		{
			name: "node with legacy instance type label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelTopologyRegion:   "us-east-1",
						common.LabelBetaInstanceType: "c5.2xlarge",
					},
				},
			},
			wantProvider:     "aws", // Will be inferred from instance type pattern
			wantRegion:       "us-east-1",
			wantInstanceType: "c5.2xlarge",
		},
		{
			name: "node with minimal labels",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelTopologyRegion: "us-west-1",
					},
				},
			},
			wantRegion: "us-west-1",
		},
		{
			name:     "nil node",
			node:     nil,
			wantProvider:     "",
			wantRegion:       "",
			wantZone:         "",
			wantInstanceType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ExtractNodeInfo(tt.node)

			if info.Provider != tt.wantProvider {
				t.Errorf("ExtractNodeInfo().Provider = %v, want %v", info.Provider, tt.wantProvider)
			}
			if info.Region != tt.wantRegion {
				t.Errorf("ExtractNodeInfo().Region = %v, want %v", info.Region, tt.wantRegion)
			}
			if info.Zone != tt.wantZone {
				t.Errorf("ExtractNodeInfo().Zone = %v, want %v", info.Zone, tt.wantZone)
			}
			if info.InstanceType != tt.wantInstanceType {
				t.Errorf("ExtractNodeInfo().InstanceType = %v, want %v", info.InstanceType, tt.wantInstanceType)
			}
		})
	}
}

func TestNodeInfo_IsComplete(t *testing.T) {
	tests := []struct {
		name string
		info NodeInfo
		want bool
	}{
		{
			name: "complete with zone",
			info: NodeInfo{
				Zone: "us-west-2a",
			},
			want: true,
		},
		{
			name: "complete with provider and region",
			info: NodeInfo{
				Provider: "aws",
				Region:   "us-west-2",
			},
			want: true,
		},
		{
			name: "complete with all fields",
			info: NodeInfo{
				Provider:     "aws",
				Region:       "us-west-2",
				Zone:         "us-west-2a",
				InstanceType: "m5.xlarge",
			},
			want: true,
		},
		{
			name: "incomplete - provider only",
			info: NodeInfo{
				Provider: "aws",
			},
			want: false,
		},
		{
			name: "incomplete - region only",
			info: NodeInfo{
				Region: "us-west-2",
			},
			want: false,
		},
		{
			name: "incomplete - instance type only",
			info: NodeInfo{
				InstanceType: "m5.xlarge",
			},
			want: false,
		},
		{
			name: "incomplete - empty",
			info: NodeInfo{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.IsComplete(); got != tt.want {
				t.Errorf("NodeInfo.IsComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInferProvider(t *testing.T) {
	tests := []struct {
		name         string
		providerID   string
		instanceType string
		labels       map[string]string
		want         string
	}{
		{
			name:       "AWS from providerID",
			providerID: "aws:///us-west-2a/i-1234567890abcdef0",
			want:       "aws",
		},
		{
			name:       "GCP from providerID",
			providerID: "gce://project/zone/instance",
			want:       "gcp",
		},
		{
			name:       "Azure from providerID",
			providerID: "azure:///subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
			want:       "azure",
		},
		{
			name:         "AWS from instance type pattern",
			instanceType: "m5.xlarge",
			want:         "aws",
		},
		{
			name:         "AWS from another instance type pattern",
			instanceType: "c6i.2xlarge",
			want:         "aws",
		},
		{
			name:         "GCP from instance type pattern",
			instanceType: "n1-standard-4",
			want:         "gcp",
		},
		{
			name:         "GCP from e2 instance type",
			instanceType: "e2-medium",
			want:         "gcp",
		},
		{
			name:         "Azure from instance type pattern",
			instanceType: "Standard_D4s_v3",
			want:         "azure",
		},
		{
			name: "AWS from EKS label",
			labels: map[string]string{
				"eks.amazonaws.com/capacityType": "SPOT",
			},
			want: "aws",
		},
		{
			name: "GCP from GKE label",
			labels: map[string]string{
				"cloud.google.com/gke-nodepool": "default-pool",
			},
			want: "gcp",
		},
		{
			name: "Azure from AKS label",
			labels: map[string]string{
				"kubernetes.azure.com/cluster": "my-cluster",
			},
			want: "azure",
		},
		{
			name:         "unable to determine",
			instanceType: "unknown-type",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: tt.providerID,
				},
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.labels,
				},
			}

			if got := inferProvider(node, tt.instanceType); got != tt.want {
				t.Errorf("inferProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}
