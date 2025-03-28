package main

import (
	"bufio"
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
)

func init() {
	// Register metrics with Prometheus
	prometheus.MustRegister(cpuFrequency)
	prometheus.MustRegister(cpuFrequencyStatic)
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

		// Get base frequency if available
		baseFiles, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/cpuinfo_base_freq")
		if err == nil && len(baseFiles) > 0 {
			data, err := os.ReadFile(baseFiles[0])
			if err == nil {
				if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
					// Convert from kHz to GHz
					result["base"] = freq / 1000000
				}
			}
		}

		// Some systems use base_frequency instead
		if _, ok := result["base"]; !ok {
			baseFiles, err := filepath.Glob("/sys/devices/system/cpu/cpu*/cpufreq/base_frequency")
			if err == nil && len(baseFiles) > 0 {
				data, err := os.ReadFile(baseFiles[0])
				if err == nil {
					if freq, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64); err == nil {
						// Convert from kHz to GHz
						result["base"] = freq / 1000000
					}
				}
			}
		}
	}

	// If base frequency not available, try to estimate from cpuinfo
	if _, ok := result["base"]; !ok {
		// Try to get base frequency from cpuinfo
		baseFreq := estimateBaseFrequencyFromCPUInfo()
		if baseFreq > 0 {
			result["base"] = baseFreq
		}
	}

	// If max frequency not available, try to estimate from cpuinfo
	if _, ok := result["max"]; !ok {
		maxFreq := estimateMaxFrequencyFromCPUInfo()
		if maxFreq > 0 {
			result["max"] = maxFreq
		}
	}

	// If min frequency not available and base is, estimate min as a fraction of base
	if _, ok := result["min"]; !ok {
		if base, ok := result["base"]; ok {
			// Many CPUs scale down to about 1/3 of base frequency
			result["min"] = base / 3
		}
	}

	return result, nil
}

// getCPUCount returns the number of CPUs (logical processors)
func getCPUCount() (int, error) {
	// Try to count the number of CPU directories
	cpuDirs, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*")
	if err != nil {
		return 0, err
	}

	return len(cpuDirs), nil
}

// getCurrentCPUFrequency gets the current frequency for a specific CPU
func getCurrentCPUFrequency(cpuID int) (float64, error) {
	// First try with the scaling_cur_freq file
	freqFile := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/scaling_cur_freq", cpuID)
	data, err := os.ReadFile(freqFile)

	// If that doesn't work, try the cpuinfo_cur_freq file
	if err != nil {
		freqFile = fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/cpuinfo_cur_freq", cpuID)
		data, err = os.ReadFile(freqFile)
		if err != nil {
			return 0, fmt.Errorf("failed to read CPU frequency: %v", err)
		}
	}

	// Parse the frequency value (in kHz)
	freqKHz, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU frequency: %v", err)
	}

	// Convert from kHz to GHz
	return freqKHz / 1000000, nil
}

// estimateBaseFrequencyFromCPUInfo tries to get base frequency from /proc/cpuinfo
func estimateBaseFrequencyFromCPUInfo() float64 {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Look for the "cpu MHz" line
		if strings.Contains(line, "cpu MHz") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				freq, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					// Convert MHz to GHz and return
					return freq / 1000
				}
			}
			break
		}

		// Also check for "model name" in case it contains the frequency
		if strings.Contains(line, "model name") && strings.Contains(line, "GHz") {
			// Extract the frequency if it's in the model name (like "Intel i7 @ 2.60GHz")
			parts := strings.Split(line, "@")
			if len(parts) >= 2 {
				// Find the GHz value
				ghzPart := parts[1]
				ghzPart = strings.TrimSpace(ghzPart)
				ghzPart = strings.Split(ghzPart, " ")[0] // Get just the number
				ghzPart = strings.Replace(ghzPart, "GHz", "", 1)

				freq, err := strconv.ParseFloat(ghzPart, 64)
				if err == nil {
					return freq
				}
			}
			break
		}
	}

	return 0
}

// estimateMaxFrequencyFromCPUInfo tries to get max frequency
func estimateMaxFrequencyFromCPUInfo() float64 {
	// Often max frequency is about 20-30% higher than base,
	// but we'd need more info to estimate accurately
	baseFreq := estimateBaseFrequencyFromCPUInfo()
	if baseFreq > 0 {
		return baseFreq * 1.2 // Very rough estimate
	}
	return 0
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
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", "", "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
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
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":9100", "The address the metric endpoint binds to")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (not needed in cluster)")
	flag.StringVar(&nodeName, "node-name", "", "Name of the node this agent is running on (defaults to environment variable NODE_NAME)")
	flag.BoolVar(&annotateOnly, "annotate-only", false, "Only annotate CPU info and exit")
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
	klog.InfoS("Starting CPU information exporter",
		"node", nodeName,
		"metricsAddr", metricsAddr,
		"annotateOnly", annotateOnly)

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

	// Annotate the node with CPU model info
	if err := annotateCPUModel(clientset, nodeName); err != nil {
		klog.ErrorS(err, "Failed to annotate node with CPU model information")
		// Continue running even if annotation fails
	}

	// If annotate-only mode, exit after annotation
	if annotateOnly {
		klog.InfoS("Annotation completed, exiting (annotate-only mode)")
		os.Exit(0)
	}

	// Start collecting metrics
	go recordCPUMetrics(nodeName)

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
