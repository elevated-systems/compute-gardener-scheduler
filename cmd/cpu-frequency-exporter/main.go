package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const (
	namespace = "compute_gardener"
	subsystem = "cpu"
)

var (
	// Prometheus metrics
	cpuFrequency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "frequency_ghz",
			Help:      "Current CPU frequency in GHz",
		},
		[]string{"cpu", "node"},
	)
	
	cpuFrequencyStatic = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
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
		result["min"] = 0.8  // Common minimum for desktop/server CPUs
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

// recordMetrics collects and records CPU frequency metrics
func recordMetrics(nodeName string) {
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
					"cpu": cpuID,
					"node": nodeName,
					"type": freqType,
				}).Set(value)
			}
		}
	}
	
	// Start periodic collection of current frequency
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
					"cpu": cpuID,
					"node": nodeName,
				}).Set(freq)
			}
		}
	}
}

func main() {
	var (
		metricsAddr   string
		kubeconfig    string
		nodeName      string
	)
	
	flag.StringVar(&metricsAddr, "metrics-addr", ":9100", "The address the metric endpoint binds to")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (not needed in cluster)")
	flag.StringVar(&nodeName, "node-name", "", "Name of the node this agent is running on (defaults to hostname)")
	klog.InitFlags(nil)
	flag.Parse()
	
	// Get node name from hostname if not provided
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			klog.ErrorS(err, "Failed to get hostname")
			os.Exit(1)
		}
		nodeName = hostname
	}
	
	// Log startup
	klog.InfoS("Starting CPU frequency exporter", 
		"node", nodeName,
		"metricsAddr", metricsAddr)
	
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
	
	_, err = kubernetes.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}
	
	// Start collecting metrics
	go recordMetrics(nodeName)
	
	// Start HTTP server for metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr: metricsAddr,
	}
	
	klog.InfoS("Starting metrics server", "addr", metricsAddr)
	if err := server.ListenAndServe(); err != nil {
		klog.ErrorS(err, "Failed to start metrics server")
		os.Exit(1)
	}
}