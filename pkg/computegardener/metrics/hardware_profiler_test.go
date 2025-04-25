package metrics

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/config"
)

func TestHardwareProfilerWithCloud(t *testing.T) {
	// Create a test hardware profiles configuration
	profiles := &config.HardwareProfiles{
		CPUProfiles: map[string]config.PowerProfile{
			"Intel(R) Xeon(R) Platinum 8175M": {
				IdlePower: 10.0,
				MaxPower:  100.0,
			},
		},
		GPUProfiles: map[string]config.PowerProfile{
			"NVIDIA V100": {
				IdlePower: 20.0,
				MaxPower:  300.0,
			},
		},
		MemProfiles: map[string]config.MemoryPowerProfile{
			"DDR4-2666 ECC": {
				IdlePowerPerGB: 0.125,
				MaxPowerPerGB:  0.375,
				BaseIdlePower:  1.0,
			},
		},
		CloudInstanceMapping: map[string]map[string]config.HardwareComponents{
			"aws": {
				"m5.large": {
					CPUModel:       "Intel(R) Xeon(R) Platinum 8175M",
					MemoryType:     "DDR4-2666 ECC",
					NumCPUs:        2,
					TotalMemory:    8192,
					MemoryChannels: 6,
				},
				"p3.2xlarge": {
					CPUModel:       "Intel(R) Xeon(R) Platinum 8175M",
					GPUModel:       "NVIDIA V100",
					MemoryType:     "DDR4-2666 ECC",
					NumCPUs:        8,
					NumGPUs:        1,
					TotalMemory:    61440,
					MemoryChannels: 6,
				},
			},
		},
	}

	// Create a hardware profiler with the test configuration
	profiler := NewHardwareProfiler(profiles)

	// Test node with AWS instance type label
	awsNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aws-node",
			Labels: map[string]string{
				"node.kubernetes.io/instance-type": "m5.large",
			},
		},
		Spec: v1.NodeSpec{
			ProviderID: "aws://us-west-2/i-1234567890abcdef0",
		},
	}

	// Detect the power profile for the AWS node
	nodePower, err := profiler.DetectNodePowerProfile(awsNode)
	if err != nil {
		t.Fatalf("Failed to detect AWS node power profile: %v", err)
	}

	// Base CPU power (10.0) + Memory power (8GB * 0.125 + 1.0 base) = ~12.0
	expectedIdlePower := 12.0
	if nodePower.IdlePower < expectedIdlePower*0.9 || nodePower.IdlePower > expectedIdlePower*1.1 {
		t.Errorf("Unexpected idle power for AWS node: got %f, expected approximately %f",
			nodePower.IdlePower, expectedIdlePower)
	}

	// Base max power (100.0) + Memory max power (8GB * 0.375 + 1.0 base) = ~104.0
	expectedMaxPower := 104.0
	if nodePower.MaxPower < expectedMaxPower*0.9 || nodePower.MaxPower > expectedMaxPower*1.1 {
		t.Errorf("Unexpected max power for AWS node: got %f, expected approximately %f",
			nodePower.MaxPower, expectedMaxPower)
	}
}

func TestHardwareProfilerWithOnPrem(t *testing.T) {
	// Create a test hardware profiles configuration
	profiles := &config.HardwareProfiles{
		CPUProfiles: map[string]config.PowerProfile{
			"Intel(R) Core(TM) i5-6500 CPU @ 3.20GHz": {
				IdlePower: 5.0,
				MaxPower:  65.0,
			},
		},
		GPUProfiles: map[string]config.PowerProfile{
			"NVIDIA GeForce GTX 1660": {
				IdlePower: 7.0,
				MaxPower:  125.0,
			},
		},
	}

	// Create a hardware profiler with the test configuration
	profiler := NewHardwareProfiler(profiles)

	// Test node with CPU model annotation
	onPremNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "on-prem-node",
			Labels: map[string]string{
				common.NFDLabelCPUModel:      "Intel(R) Core(TM) i5-6500 CPU @ 3.20GHz",
				common.NvidiaLabelGPUProduct: "NVIDIA GeForce GTX 1660",
			},
		},
	}

	// Detect the power profile for the on-premises node
	nodePower, err := profiler.DetectNodePowerProfile(onPremNode)
	if err != nil {
		t.Fatalf("Failed to detect on-prem node power profile: %v", err)
	}

	// Check CPU power values
	if nodePower.IdlePower != 5.0 {
		t.Errorf("Unexpected idle power: got %f, expected 5.0", nodePower.IdlePower)
	}
	if nodePower.MaxPower != 65.0 {
		t.Errorf("Unexpected max power: got %f, expected 65.0", nodePower.MaxPower)
	}

	// Check GPU power values
	if nodePower.IdleGPUPower != 7.0 {
		t.Errorf("Unexpected idle GPU power: got %f, expected 7.0", nodePower.IdleGPUPower)
	}
	if nodePower.MaxGPUPower != 125.0 {
		t.Errorf("Unexpected max GPU power: got %f, expected 125.0", nodePower.MaxGPUPower)
	}
}

func TestHardwareProfilerFallback(t *testing.T) {
	// Create a test hardware profiles configuration
	profiles := &config.HardwareProfiles{
		CPUProfiles: map[string]config.PowerProfile{
			"Intel(R) Core(TM) i5-6500 CPU @ 3.20GHz": {
				IdlePower: 5.0,
				MaxPower:  65.0,
			},
		},
	}

	// Create a hardware profiler with the test configuration
	profiler := NewHardwareProfiler(profiles)

	// Test node with unknown hardware
	unknownNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unknown-node",
		},
	}

	// Detect the power profile for the unknown node - should return an error
	_, err := profiler.DetectNodePowerProfile(unknownNode)
	if err == nil {
		t.Errorf("Expected error for unknown node, but got none")
	}
}
