package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	namespace    = "compute_gardener"
	cpuSubsystem = "cpu"
	gpuSubsystem = "gpu"
)

var (
	// CPU Prometheus metrics
	cpuFrequency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: cpuSubsystem,
			Name:      "frequency_ghz",
			Help:      "Current CPU frequency in GHz",
		},
		[]string{"cpu", "node"},
	)

	cpuFrequencyStatic = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: cpuSubsystem,
			Name:      "frequency_static_ghz",
			Help:      "Static CPU frequency information (base, min, max) in GHz",
		},
		[]string{"cpu", "node", "type"},
	)

	// GPU Prometheus metrics - Dynamic
	gpuPower = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: gpuSubsystem,
			Name:      "power_watts",
			Help:      "Current GPU power consumption in watts",
		},
		[]string{"gpu", "node"},
	)

	gpuUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: gpuSubsystem,
			Name:      "utilization_percent",
			Help:      "Current GPU utilization percentage",
		},
		[]string{"gpu", "node"},
	)

	gpuMemoryUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: gpuSubsystem,
			Name:      "memory_utilization_percent",
			Help:      "Current GPU memory utilization percentage",
		},
		[]string{"gpu", "node"},
	)

	// GPU Prometheus metrics - Static
	gpuCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: gpuSubsystem,
			Name:      "count",
			Help:      "Number of GPUs detected on the node",
		},
	)

	gpuMaxMemory = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: gpuSubsystem,
			Name:      "max_memory_bytes",
			Help:      "Maximum memory capacity of the GPU in bytes",
		},
		[]string{"gpu", "node"},
	)

	gpuMaxPower = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: gpuSubsystem,
			Name:      "max_power_watts",
			Help:      "Maximum power limit of the GPU in watts",
		},
		[]string{"gpu", "node"},
	)
)

func init() {
	// Register metrics with Prometheus
	// CPU metrics
	prometheus.MustRegister(cpuFrequency)
	prometheus.MustRegister(cpuFrequencyStatic)

	// GPU metrics - Dynamic
	prometheus.MustRegister(gpuPower)
	prometheus.MustRegister(gpuUtilization)
	prometheus.MustRegister(gpuMemoryUtilization)

	// GPU metrics - Static
	prometheus.MustRegister(gpuCount)
	prometheus.MustRegister(gpuMaxMemory)
	prometheus.MustRegister(gpuMaxPower)
}

// getStaticCPUFrequencyInfo retrieves static CPU frequency information
func getStaticCPUFrequencyInfo() (map[string]float64, error) {
	result := make(map[string]float64)

	// First try to get min/max from cpufreq interface
	cpuDirs, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq")
	if err == nil && len(cpuDirs) > 0 {
		// Get min frequency
		minFiles, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/scaling_min_freq")
		if err == nil && len(minFiles) > 0 {
			data, err := os.ReadFile(minFiles[0])
			if err == nil {
				if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					// Convert from kHz to GHz
					result["min"] = freq / 1000000
				}
			}
		}

		// Get max frequency
		maxFiles, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/scaling_max_freq")
		if err == nil && len(maxFiles) > 0 {
			data, err := os.ReadFile(maxFiles[0])
			if err == nil {
				if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					// Convert from kHz to GHz
					result["max"] = freq / 1000000
				}
			}
		}

		// Get base frequency (cpuinfo_base_freq if available)
		baseFiles, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_base_freq")
		if err == nil && len(baseFiles) > 0 {
			data, err := os.ReadFile(baseFiles[0])
			if err == nil {
				if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					// Convert from kHz to GHz
					result["base"] = freq / 1000000
				}
			}
		} else {
			// If base frequency file isn't available, estimate from model name
			result["base"] = estimateBaseFrequencyFromCPUInfo()
		}
	} else {
		// Fallback - estimate from CPU info
		result["min"] = 0.8 // Common minimum for desktop/server CPUs
		result["max"] = estimateMaxFrequencyFromCPUInfo()
		result["base"] = estimateBaseFrequencyFromCPUInfo()
	}

	return result, nil
}

// getCPUCount returns the number of CPU cores
func getCPUCount() (int, error) {
	// Try reading from /proc/cpuinfo
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc/cpuinfo: %v", err)
	}

	// Count "processor" lines
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("no CPU processors found in /proc/cpuinfo")
	}

	return count, nil
}

// getCurrentCPUFrequency retrieves the current CPU frequency in GHz for a specific core
func getCurrentCPUFrequency(cpuID int) (float64, error) {
	// Try cpufreq interface first (most accurate)
	freqFile := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/scaling_cur_freq", cpuID)
	data, err := os.ReadFile(freqFile)
	if err == nil {
		if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
			// Convert from kHz to GHz
			return freq / 1000000, nil
		}
	}

	// Fall back to /proc/cpuinfo
	data, err = os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, fmt.Errorf("unable to read /proc/cpuinfo: %v", err)
	}

	// Find the frequency for the specific CPU
	cpuFound := false
	var cpuFreq float64

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()

		// Check for processor ID
		if strings.HasPrefix(line, "processor") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				if id, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					cpuFound = (id == cpuID)
				}
			}
		}

		// If we found the CPU we're looking for, check for its frequency
		if cpuFound && strings.Contains(line, "cpu MHz") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				if freq, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
					// Convert from MHz to GHz
					cpuFreq = freq / 1000
					return cpuFreq, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("unable to determine CPU frequency for CPU %d", cpuID)
}

// estimateBaseFrequencyFromCPUInfo tries to extract base frequency from CPU model name
func estimateBaseFrequencyFromCPUInfo() float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "model name") {
			// Look for GHz value in model name
			modelName := strings.ToLower(line)
			// Extract frequency from strings like "@ 3.20GHz"
			if idx := strings.Index(modelName, "@ "); idx != -1 {
				freqStr := modelName[idx+2:]
				// Find where GHz or MHz is mentioned
				ghzIdx := strings.Index(freqStr, "ghz")
				if ghzIdx != -1 {
					freqVal := freqStr[:ghzIdx]
					if freq, err := strconv.ParseFloat(strings.TrimSpace(freqVal), 64); err == nil {
						return freq
					}
				}
				mhzIdx := strings.Index(freqStr, "mhz")
				if mhzIdx != -1 {
					freqVal := freqStr[:mhzIdx]
					if freq, err := strconv.ParseFloat(strings.TrimSpace(freqVal), 64); err == nil {
						return freq / 1000 // Convert MHz to GHz
					}
				}
			}

			// If we can't find an explicit frequency, make an educated guess based on model
			if strings.Contains(modelName, "i9") {
				return 3.6
			} else if strings.Contains(modelName, "i7") {
				return 3.4
			} else if strings.Contains(modelName, "i5") {
				return 3.2
			} else if strings.Contains(modelName, "i3") {
				return 3.0
			} else if strings.Contains(modelName, "xeon") {
				return 2.5
			}
		}
	}

	// Default fallback
	return 2.0
}

// estimateMaxFrequencyFromCPUInfo estimates max turbo frequency
func estimateMaxFrequencyFromCPUInfo() float64 {
	// Get base frequency and add typical turbo headroom
	base := estimateBaseFrequencyFromCPUInfo()
	if base <= 0 {
		return 3.0 // Default fallback
	}

	// Typical turbo boost is about 10-20% over base
	return base * 1.15
}

// findNvidiaSmi tries to locate the nvidia-smi binary in various common locations
func findNvidiaSmi() string {
	// First try via PATH - this already handles symlinks
	nvidiaSmi, err := exec.LookPath("nvidia-smi")
	if err == nil {
		klog.V(2).InfoS("Found nvidia-smi in PATH", "path", nvidiaSmi)
		return nvidiaSmi
	}

	// Try common alternative names that might be symlinked
	alternativeNames := []string{
		"nvidia-smi",
		"nvidia-sm", // Some systems abbreviate
		"smi",       // Some container environments simplify
		"nvidia",    // Some systems use just nvidia as the command
	}

	for _, name := range alternativeNames {
		if path, err := exec.LookPath(name); err == nil {
			klog.V(2).InfoS("Found nvidia-smi via alternative name", "name", name, "path", path)
			return path
		}
	}

	// Common locations for nvidia-smi
	commonLocations := []string{
		"/usr/bin/nvidia-smi",
		"/usr/local/bin/nvidia-smi",
		"/usr/local/nvidia/bin/nvidia-smi",
		"/opt/nvidia/nvidia-smi",
		"/usr/lib/nvidia-smi",
		"/usr/lib/nvidia/nvidia-smi",
		"/usr/lib64/nvidia/bin/nvidia-smi",
		"/usr/local/cuda/bin/nvidia-smi",    // CUDA installation
		"/opt/nvidia/containers/nvidia-smi", // Container specific path
		"/var/lib/nvidia/bin/nvidia-smi",    // Another common location
		// Add more common locations if needed
	}

	// Check each location - os.Stat follows symlinks
	for _, location := range commonLocations {
		if fileInfo, err := os.Stat(location); err == nil && !fileInfo.IsDir() {
			klog.V(2).InfoS("Found nvidia-smi in alternate location", "path", location)
			return location
		}
	}

	// Try to use find command to locate nvidia-smi in common parent directories
	findCommands := []string{
		"find /usr -name nvidia-smi -type f -o -type l 2>/dev/null | head -1",
		"find /opt -name nvidia-smi -type f -o -type l 2>/dev/null | head -1",
		"find / -name nvidia-smi -type f -o -type l -path '*/bin/*' 2>/dev/null | head -1",
	}

	for _, findCmd := range findCommands {
		cmd := exec.Command("bash", "-c", findCmd)
		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			path := strings.TrimSpace(string(output))
			if path != "" {
				klog.V(2).InfoS("Found nvidia-smi using find command", "path", path)
				return path
			}
		}
	}

	// Try to find it using the 'which' command as a fallback
	cmd := exec.Command("bash", "-c", "which nvidia-smi")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		path := strings.TrimSpace(string(output))
		klog.V(2).InfoS("Found nvidia-smi using 'which'", "path", path)
		return path
	}

	klog.V(2).InfoS("nvidia-smi not found in any common location")
	return ""
}

// hasNvidiaGPU checks if the node has NVIDIA GPUs installed
func hasNvidiaGPU() bool {
	// Try to find nvidia-smi
	nvidiaSmi := findNvidiaSmi()
	if nvidiaSmi == "" {
		klog.V(2).InfoS("nvidia-smi not found, assuming no NVIDIA GPUs")
		return false
	}

	// Try to get GPU count
	cmd := exec.Command(nvidiaSmi, "--query-gpu=count", "--format=csv,noheader")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		// Log detailed error information
		klog.V(2).InfoS("Failed to run nvidia-smi, assuming no NVIDIA GPUs",
			"path", nvidiaSmi,
			"error", err,
			"stderr", stderr.String(),
			"exitCode", cmd.ProcessState.ExitCode())

		// Additional debugging - check if we're running in the nvidia runtime
		runtimeCmd := exec.Command("bash", "-c", "cat /proc/self/mountinfo | grep nvidia")
		runtimeOutput, _ := runtimeCmd.CombinedOutput()
		klog.V(2).InfoS("Nvidia runtime check", "found", len(runtimeOutput) > 0)
		if len(runtimeOutput) > 0 {
			klog.V(3).InfoS("Runtime details", "output", string(runtimeOutput))
		}

		return false
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || count == 0 {
		klog.V(2).InfoS("No NVIDIA GPUs detected with nvidia-smi",
			"output", string(output))
		return false
	}

	klog.V(2).InfoS("NVIDIA GPUs detected", "count", count)
	return true
}

// getGPUCount gets the number of NVIDIA GPUs on the node
func getGPUCount() (int, error) {
	nvidiaSmi := findNvidiaSmi()
	if nvidiaSmi == "" {
		return 0, fmt.Errorf("nvidia-smi not found in any common location")
	}

	cmd := exec.Command(nvidiaSmi, "--query-gpu=count", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to run nvidia-smi: %v", err)
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU count: %v", err)
	}

	return count, nil
}

// getGPUStaticInfo gets static information about each GPU
func getGPUStaticInfo() ([]map[string]float64, error) {
	nvidiaSmi := findNvidiaSmi()
	if nvidiaSmi == "" {
		return nil, fmt.Errorf("nvidia-smi not found in any common location")
	}

	// Query for total memory and power limit
	cmd := exec.Command(nvidiaSmi, "--query-gpu=index,memory.total,power.limit", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvidia-smi for static info: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	result := make([]map[string]float64, 0, len(lines))

	for _, line := range lines {
		fields := strings.Split(line, ", ")
		if len(fields) != 3 {
			klog.V(2).InfoS("Unexpected nvidia-smi output format", "line", line)
			continue
		}

		// Parse fields
		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			klog.V(2).InfoS("Failed to parse GPU index", "value", fields[0], "error", err)
			continue
		}

		// Parse memory (format: "16384 MiB")
		memParts := strings.Split(strings.TrimSpace(fields[1]), " ")
		if len(memParts) != 2 {
			klog.V(2).InfoS("Unexpected memory format", "value", fields[1])
			continue
		}

		memory, err := strconv.ParseFloat(memParts[0], 64)
		if err != nil {
			klog.V(2).InfoS("Failed to parse GPU memory", "value", memParts[0], "error", err)
			continue
		}

		// Convert MiB to bytes
		memoryBytes := memory * 1024 * 1024

		// Parse power limit (format: "250.00 W")
		powerParts := strings.Split(strings.TrimSpace(fields[2]), " ")
		if len(powerParts) != 2 {
			klog.V(2).InfoS("Unexpected power format", "value", fields[2])
			continue
		}

		power, err := strconv.ParseFloat(powerParts[0], 64)
		if err != nil {
			klog.V(2).InfoS("Failed to parse GPU power limit", "value", powerParts[0], "error", err)
			continue
		}

		// Add to result
		if index >= len(result) {
			// Resize result slice if needed
			newSize := index + 1
			newResult := make([]map[string]float64, newSize)
			copy(newResult, result)
			result = newResult
		}

		result[index] = map[string]float64{
			"maxMemory": memoryBytes,
			"maxPower":  power,
		}
	}

	return result, nil
}

// getCurrentGPUMetrics gets current utilization metrics for each GPU
func getCurrentGPUMetrics() ([]map[string]float64, error) {
	nvidiaSmi := findNvidiaSmi()
	if nvidiaSmi == "" {
		return nil, fmt.Errorf("nvidia-smi not found in any common location")
	}

	// Query for utilization and power usage
	cmd := exec.Command(nvidiaSmi, "--query-gpu=index,utilization.gpu,utilization.memory,power.draw", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run nvidia-smi for current metrics: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	result := make([]map[string]float64, 0, len(lines))

	for _, line := range lines {
		fields := strings.Split(line, ", ")
		if len(fields) != 4 {
			klog.V(2).InfoS("Unexpected nvidia-smi output format", "line", line)
			continue
		}

		// Parse fields
		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			klog.V(2).InfoS("Failed to parse GPU index", "value", fields[0], "error", err)
			continue
		}

		// Parse GPU utilization (format: "30 %")
		gpuUtilParts := strings.Split(strings.TrimSpace(fields[1]), " ")
		if len(gpuUtilParts) != 2 {
			klog.V(2).InfoS("Unexpected GPU utilization format", "value", fields[1])
			continue
		}

		gpuUtil, err := strconv.ParseFloat(gpuUtilParts[0], 64)
		if err != nil {
			klog.V(2).InfoS("Failed to parse GPU utilization", "value", gpuUtilParts[0], "error", err)
			continue
		}

		// Parse memory utilization (format: "10 %")
		memUtilParts := strings.Split(strings.TrimSpace(fields[2]), " ")
		if len(memUtilParts) != 2 {
			klog.V(2).InfoS("Unexpected memory utilization format", "value", fields[2])
			continue
		}

		memUtil, err := strconv.ParseFloat(memUtilParts[0], 64)
		if err != nil {
			klog.V(2).InfoS("Failed to parse memory utilization", "value", memUtilParts[0], "error", err)
			continue
		}

		// Parse power draw (format: "100.25 W")
		powerDrawParts := strings.Split(strings.TrimSpace(fields[3]), " ")
		if len(powerDrawParts) != 2 {
			klog.V(2).InfoS("Unexpected power draw format", "value", fields[3])
			continue
		}

		powerDraw, err := strconv.ParseFloat(powerDrawParts[0], 64)
		if err != nil {
			klog.V(2).InfoS("Failed to parse power draw", "value", powerDrawParts[0], "error", err)
			continue
		}

		// Add to result
		if index >= len(result) {
			// Resize result slice if needed
			newSize := index + 1
			newResult := make([]map[string]float64, newSize)
			copy(newResult, result)
			result = newResult
		}

		result[index] = map[string]float64{
			"utilization":       gpuUtil,
			"memoryUtilization": memUtil,
			"powerDraw":         powerDraw,
		}
	}

	return result, nil
}

// annotateNodeGPUInfo adds GPU information to the node as annotations
func annotateNodeGPUInfo(clientset *kubernetes.Clientset, nodeName string) error {
	// Check if NVIDIA GPUs are available
	if !hasNvidiaGPU() {
		klog.V(2).InfoS("No NVIDIA GPUs detected, skipping GPU annotations")
		return nil
	}

	// Get GPU count
	gpuCount, err := getGPUCount()
	if err != nil {
		return fmt.Errorf("failed to get GPU count: %v", err)
	}

	// Get static GPU info
	gpuInfo, err := getGPUStaticInfo()
	if err != nil {
		return fmt.Errorf("failed to get GPU static info: %v", err)
	}

	// Compile GPU model names
	gpuModels := make([]string, 0, len(gpuInfo))
	totalMaxPower := 0.0

	// Get GPU model names
	nvidiaSmi := findNvidiaSmi()
	if nvidiaSmi != "" {
		cmd := exec.Command(nvidiaSmi, "--query-gpu=name", "--format=csv,noheader")
		output, err := cmd.Output()
		if err == nil {
			models := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, model := range models {
				gpuModels = append(gpuModels, strings.TrimSpace(model))
			}
		}
	}

	// Calculate total max power
	for _, info := range gpuInfo {
		if power, ok := info["maxPower"]; ok {
			totalMaxPower += power
		}
	}

	// Get node
	node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %v", nodeName, err)
	}

	// Create a copy of the node with updated annotations
	nodeCopy := node.DeepCopy()
	if nodeCopy.Annotations == nil {
		nodeCopy.Annotations = make(map[string]string)
	}

	// Add GPU annotations
	nodeCopy.Annotations[common.AnnotationGPUCount] = fmt.Sprintf("%d", gpuCount)
	nodeCopy.Annotations[common.AnnotationGPUTotalPower] = fmt.Sprintf("%.2f", totalMaxPower)

	if len(gpuModels) > 0 {
		// Join model names with comma
		nodeCopy.Annotations[common.AnnotationGPUModel] = strings.Join(gpuModels, ",")
	}

	// Update the node
	_, err = clientset.CoreV1().Nodes().Update(context.Background(), nodeCopy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node annotations: %v", err)
	}

	klog.V(2).InfoS("Successfully annotated node with GPU information",
		"node", nodeName,
		"gpuCount", gpuCount,
		"gpuModels", gpuModels,
		"totalMaxPower", totalMaxPower)

	return nil
}

// recordCPUMetrics collects and records CPU-specific metrics
func recordCPUMetrics(nodeName string) {
	// Get CPU count
	cpuCount, err := getCPUCount()
	if err != nil {
		klog.ErrorS(err, "Failed to get CPU count")
		cpuCount = 1 // Assume at least one CPU
	}

	// Get static frequency information (only once at startup)
	staticInfo, err := getStaticCPUFrequencyInfo()
	if err != nil {
		klog.ErrorS(err, "Failed to get static CPU frequency information")
	} else {
		// Record static frequency information for all CPUs
		for i := 0; i < cpuCount; i++ {
			cpuID := fmt.Sprintf("%d", i)
			for freqType, value := range staticInfo {
				klog.V(2).InfoS("Recorded static CPU frequency",
					"cpu", cpuID,
					"type", freqType,
					"frequency", value)
				cpuFrequencyStatic.With(prometheus.Labels{
					"cpu":  cpuID,
					"node": nodeName,
					"type": freqType,
				}).Set(value)
			}
		}
	}

	// Start periodic collection of current metrics
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Record current frequency for each CPU
			for i := 0; i < cpuCount; i++ {
				freq, err := getCurrentCPUFrequency(i)
				if err != nil {
					klog.V(2).ErrorS(err, "Failed to get current CPU frequency", "cpu", i)
					continue
				}

				cpuID := fmt.Sprintf("%d", i)
				klog.V(2).InfoS("Recorded current CPU frequency",
					"cpu", cpuID,
					"frequency", freq)
				cpuFrequency.With(prometheus.Labels{
					"cpu":  cpuID,
					"node": nodeName,
				}).Set(freq)
			}
		}
	}
}

// recordGPUMetrics collects and records GPU-specific metrics
func recordGPUMetrics(nodeName string) {
	// Additional diagnostics in GPU mode to help debug issues
	nvidiaSmiPath := findNvidiaSmi()
	if nvidiaSmiPath != "" {
		// Let's check if the file is really executable
		fileInfo, err := os.Stat(nvidiaSmiPath)
		if err != nil {
			klog.InfoS("nvidia-smi stat error", "path", nvidiaSmiPath, "error", err)
		} else {
			klog.InfoS("nvidia-smi file info", 
				"path", nvidiaSmiPath,
				"mode", fileInfo.Mode().String(),
				"size", fileInfo.Size(),
				"isDir", fileInfo.IsDir())
		}
		
		// Try to list the directory contents to see what's available
		dirPath := filepath.Dir(nvidiaSmiPath)
		files, err := os.ReadDir(dirPath)
		if err != nil {
			klog.InfoS("Failed to read nvidia-smi directory", "dir", dirPath, "error", err)
		} else {
			fileList := make([]string, 0, len(files))
			for _, f := range files {
				fileList = append(fileList, f.Name())
			}
			klog.InfoS("Directory contents", "dir", dirPath, "files", strings.Join(fileList, ", "))
		}
		
		// Check libs to see if NVIDIA libs are available
		cmd := exec.Command("bash", "-c", "ldd " + nvidiaSmiPath + " || true")
		output, _ := cmd.CombinedOutput()
		klog.InfoS("nvidia-smi library dependencies", "output", string(output))
		
		// Check environment variables
		envOutput, _ := exec.Command("bash", "-c", "env | grep -i nvidia || true").CombinedOutput()
		klog.InfoS("NVIDIA environment variables", "env", string(envOutput))
		
		// Try to find and link important NVIDIA libraries
		potentialLibPaths := []string{
			"/usr/lib", "/usr/lib64", 
			"/host-lib", "/host-lib64",
			"/usr/local/nvidia/lib", "/usr/local/nvidia/lib64",
			"/run/nvidia/driver/lib", "/run/nvidia/driver/lib64",
			"/usr/lib/nvidia", "/usr/lib64/nvidia",
		}
		
		// Check for NVIDIA libraries in potential locations
		for _, libDir := range potentialLibPaths {
			output, _ := exec.Command("bash", "-c", "ls -la " + libDir + "/libnvidia-ml* 2>/dev/null || true").CombinedOutput()
			if len(output) > 0 {
				klog.InfoS("Found NVIDIA libraries", "path", libDir, "output", string(output))
				
				// Try to create symlinks to libraries for nvidia-smi
				linkCmd := exec.Command("bash", "-c", "ln -sf " + libDir + "/libnvidia-ml* /usr/lib/ 2>/dev/null || true")
				linkCmd.Run() // Ignore errors
				
				// Log what we did
				klog.InfoS("Created symlinks to NVIDIA libraries", "from", libDir, "to", "/usr/lib/")
			}
		}
		
		// Check LD_LIBRARY_PATH and search paths
		ldOutput, _ := exec.Command("bash", "-c", "echo $LD_LIBRARY_PATH").CombinedOutput()
		klog.InfoS("LD_LIBRARY_PATH", "value", strings.TrimSpace(string(ldOutput)))
		
		// Check library configuration
		ldConfigOutput, _ := exec.Command("bash", "-c", "ldconfig -p | grep -i nvidia || true").CombinedOutput()
		klog.InfoS("NVIDIA libraries in ldconfig", "output", string(ldConfigOutput))
	}

	// Verify that NVIDIA GPUs are available
	if !hasNvidiaGPU() {
		klog.ErrorS(fmt.Errorf("no NVIDIA GPUs detected"), "Cannot collect GPU metrics in GPU mode")
		
		// Additional diagnostics for GPU detection
		klog.InfoS("Checking device files", "command", "ls -la /dev/nvidia*")
		devOutput, _ := exec.Command("bash", "-c", "ls -la /dev/nvidia* 2>/dev/null || true").CombinedOutput()
		klog.InfoS("GPU device files", "output", string(devOutput))

		// Check PCI devices
		pciOutput, _ := exec.Command("bash", "-c", "lspci | grep -i nvidia || true").CombinedOutput()
		klog.InfoS("PCI devices", "nvidia_devices", string(pciOutput))

		// We'll still keep running, but won't collect any metrics
		select {} // Block forever
		return
	}

	// Get GPU count and set metric
	count, err := getGPUCount()
	if err != nil {
		klog.ErrorS(err, "Failed to get GPU count")
	} else {
		gpuCount.Set(float64(count))
		klog.V(2).InfoS("Set GPU count metric", "count", count)
	}

	// Get static GPU information
	gpuStatic, err := getGPUStaticInfo()
	if err != nil {
		klog.ErrorS(err, "Failed to get static GPU information")
	} else {
		// Record static information for each GPU
		for i, info := range gpuStatic {
			gpuID := fmt.Sprintf("%d", i)

			if maxMem, ok := info["maxMemory"]; ok {
				gpuMaxMemory.With(prometheus.Labels{
					"gpu":  gpuID,
					"node": nodeName,
				}).Set(maxMem)
				klog.V(2).InfoS("Recorded static GPU memory", "gpu", gpuID, "maxMemory", maxMem)
			}

			if maxPower, ok := info["maxPower"]; ok {
				gpuMaxPower.With(prometheus.Labels{
					"gpu":  gpuID,
					"node": nodeName,
				}).Set(maxPower)
				klog.V(2).InfoS("Recorded static GPU power limit", "gpu", gpuID, "maxPower", maxPower)
			}
		}
	}

	// Start periodic collection of current metrics
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			gpuMetrics, err := getCurrentGPUMetrics()
			if err != nil {
				klog.V(2).ErrorS(err, "Failed to get current GPU metrics")
				continue
			}

			// Record metrics for each GPU
			for i, metrics := range gpuMetrics {
				gpuID := fmt.Sprintf("%d", i)

				if util, ok := metrics["utilization"]; ok {
					gpuUtilization.With(prometheus.Labels{
						"gpu":  gpuID,
						"node": nodeName,
					}).Set(util)
					klog.V(2).InfoS("Recorded GPU utilization", "gpu", gpuID, "utilization", util)
				}

				if memUtil, ok := metrics["memoryUtilization"]; ok {
					gpuMemoryUtilization.With(prometheus.Labels{
						"gpu":  gpuID,
						"node": nodeName,
					}).Set(memUtil)
					klog.V(2).InfoS("Recorded GPU memory utilization", "gpu", gpuID, "memoryUtilization", memUtil)
				}

				if power, ok := metrics["powerDraw"]; ok {
					gpuPower.With(prometheus.Labels{
						"gpu":  gpuID,
						"node": nodeName,
					}).Set(power)
					klog.V(2).InfoS("Recorded GPU power draw", "gpu", gpuID, "powerDraw", power)
				}
			}
		}
	}
}

// getCPUModelInfo retrieves detailed CPU model information
func getCPUModelInfo() (model, vendor, family string, err error) {
	// Try to get CPU model info from lscpu if available (more structured)
	lscpuPath, err := exec.LookPath("lscpu")
	if err == nil {
		cmd := exec.Command(lscpuPath)
		output, err := cmd.Output()
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(output)))
			for scanner.Scan() {
				line := scanner.Text()
				if strings.Contains(line, "Vendor ID:") {
					vendor = strings.TrimSpace(strings.Split(line, ":")[1])
				} else if strings.Contains(line, "Model name:") {
					model = strings.TrimSpace(strings.Split(line, ":")[1])
				} else if strings.Contains(line, "CPU family:") {
					family = strings.TrimSpace(strings.Split(line, ":")[1])
				}
			}

			if model != "" {
				return model, vendor, family, nil
			}
		}
	}

	// Fallback to /proc/cpuinfo
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read /proc/cpuinfo: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				model = strings.TrimSpace(parts[1])
			}
		} else if strings.Contains(line, "vendor_id") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				vendor = strings.TrimSpace(parts[1])
			}
		} else if strings.Contains(line, "cpu family") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				family = strings.TrimSpace(parts[1])
			}
		}
	}

	if model == "" {
		return "", "", "", fmt.Errorf("could not determine CPU model")
	}

	return model, vendor, family, nil
}

// annotateCPUModel adds CPU model information to the node as annotations
func annotateCPUModel(clientset *kubernetes.Clientset, nodeName string) error {
	// Get CPU model info
	cpuModel, cpuVendor, cpuFamily, err := getCPUModelInfo()
	if err != nil {
		return fmt.Errorf("failed to get CPU model info: %v", err)
	}

	// Log what we found
	klog.InfoS("Detected CPU information",
		"node", nodeName,
		"model", cpuModel,
		"vendor", cpuVendor,
		"family", cpuFamily)

	// Get node
	node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %v", nodeName, err)
	}

	// Check if annotations already exist and match
	if val, exists := node.Annotations[common.AnnotationCPUModel]; exists && val == cpuModel {
		klog.V(2).InfoS("Node already has correct CPU model annotation",
			"node", nodeName,
			"model", cpuModel)
		return nil
	}

	// Create a copy of the node with updated annotations
	nodeCopy := node.DeepCopy()
	if nodeCopy.Annotations == nil {
		nodeCopy.Annotations = make(map[string]string)
	}

	// Add annotations
	nodeCopy.Annotations[common.AnnotationCPUModel] = cpuModel

	// Update the node
	_, err = clientset.CoreV1().Nodes().Update(context.Background(), nodeCopy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update node annotations: %v", err)
	}

	klog.InfoS("Successfully annotated node with CPU model information",
		"node", nodeName,
		"model", cpuModel)

	return nil
}

func main() {
	var (
		metricsAddr  string
		kubeconfig   string
		nodeName     string
		annotateOnly bool
		mode         string
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":9100", "The address the metric endpoint binds to")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (not needed in cluster)")
	flag.StringVar(&nodeName, "node-name", "", "Name of the node this agent is running on (defaults to environment variable NODE_NAME)")
	flag.BoolVar(&annotateOnly, "annotate-only", false, "Only annotate CPU info and exit")
	flag.StringVar(&mode, "mode", "cpu", "Operation mode: 'cpu' for CPU metrics or 'gpu' for GPU metrics")
	klog.InitFlags(nil)
	flag.Parse()

	// Get node name from environment variable if not provided
	if nodeName == "" {
		// In Kubernetes, the downward API can set this environment variable
		nodeName = os.Getenv("NODE_NAME")

		// If still empty, try getting the hostname as last resort
		if nodeName == "" {
			hostname, err := os.Hostname()
			if err != nil {
				klog.ErrorS(err, "Failed to get hostname")
				os.Exit(1)
			}
			// Warn that this is not reliable in Kubernetes
			klog.Warning("Using hostname as node name - this may not be correct in Kubernetes. Use NODE_NAME env var or --node-name flag instead.", "hostname", hostname)
			nodeName = hostname
		}
	}

	// Log startup
	klog.InfoS("Starting node exporter",
		"node", nodeName,
		"metricsAddr", metricsAddr,
		"annotateOnly", annotateOnly,
		"mode", mode)

	// Create Kubernetes client
	var config *rest.Config
	var err error

	if kubeconfig == "" {
		// In-cluster configuration
		config, err = rest.InClusterConfig()
		if err != nil {
			klog.ErrorS(err, "Failed to create in-cluster config")
			os.Exit(1)
		}
	} else {
		// Out-of-cluster configuration
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.ErrorS(err, "Failed to create out-of-cluster config")
			os.Exit(1)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	// Decide which annotations to make based on mode
	if mode == "cpu" {
		// In CPU mode: Annotate CPU information only
		if err := annotateCPUModel(clientset, nodeName); err != nil {
			klog.ErrorS(err, "Failed to annotate node with CPU model information")
			// Continue running even if annotation fails
		}
	} else if mode == "gpu" {
		// In GPU mode: Annotate GPU information only
		if err := annotateNodeGPUInfo(clientset, nodeName); err != nil {
			klog.ErrorS(err, "Failed to annotate node with GPU information")
			// Continue running even if annotation fails
		}
	} else {
		klog.ErrorS(fmt.Errorf("invalid mode: %s", mode), "Unknown operation mode, must be 'cpu' or 'gpu'")
		os.Exit(1)
	}

	// If annotate-only mode, exit after annotation
	if annotateOnly {
		klog.InfoS("Annotation completed, exiting (annotate-only mode)")
		os.Exit(0)
	}

	// Start collecting metrics based on mode
	if mode == "cpu" {
		// In CPU mode: Collect CPU metrics only
		go recordCPUMetrics(nodeName)
		klog.V(2).InfoS("Running in CPU mode, collecting CPU metrics only")
	} else if mode == "gpu" {
		// In GPU mode: Collect GPU metrics only
		go recordGPUMetrics(nodeName)
		klog.V(2).InfoS("Running in GPU mode, collecting GPU metrics only")
	}

	// Start HTTP server for metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr: metricsAddr,
	}

	klog.V(1).InfoS("Starting metrics server", "addr", metricsAddr)
	if err := server.ListenAndServe(); err != nil {
		klog.ErrorS(err, "Failed to start metrics server")
		os.Exit(1)
	}
}
