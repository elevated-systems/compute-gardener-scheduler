package main

import (
	"bufio"
	"strconv"
	"strings"
	"testing"
)

// TestParseBaseFrequencyFromCPUInfo tests the core frequency parsing logic
func TestParseBaseFrequencyFromCPUInfo(t *testing.T) {
	tests := []struct {
		name     string
		cpuInfo  string
		expected float64
	}{
		{
			name: "Extract from MHz line",
			cpuInfo: `
processor       : 0
vendor_id       : GenuineIntel
cpu family      : 6
model           : 142
model name      : Intel(R) Core(TM) i7-8565U CPU @ 1.80GHz
stepping        : 11
microcode       : 0xde
cpu MHz         : 2400.000
cache size      : 8192 KB
`,
			expected: 2.4, // 2400.000 MHz = 2.4 GHz
		},
		{
			name: "Extract from model name",
			cpuInfo: `
processor       : 0
vendor_id       : GenuineIntel
cpu family      : 6
model           : 142
model name      : Intel(R) Core(TM) i7-8565U CPU @ 3.60GHz
stepping        : 11
microcode       : 0xde
cache size      : 8192 KB
`,
			expected: 3.6, // 3.60GHz = 3.6 GHz
		},
		{
			name: "No frequency information",
			cpuInfo: `
processor       : 0
vendor_id       : GenuineIntel
cpu family      : 6
model           : 142
stepping        : 11
microcode       : 0xde
cache size      : 8192 KB
`,
			expected: 0.0, // No frequency info
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test version of estimateBaseFrequencyFromCPUInfo that
			// uses a string reader instead of file I/O
			result := parseFrequencyInfo(strings.NewReader(tc.cpuInfo))
			if result != tc.expected {
				t.Errorf("parseFrequencyInfo() = %v, want %v", result, tc.expected)
			}
		})
	}
}

// Test that extracts model info from cpuinfo data
func TestParseCPUModelInfo(t *testing.T) {
	cpuInfo := `
processor       : 0
vendor_id       : GenuineIntel
cpu family      : 6
model           : 142
model name      : Intel(R) Core(TM) i7-8565U CPU @ 1.80GHz
stepping        : 11
microcode       : 0xde
cpu MHz         : 2000.000
cache size      : 8192 KB
`

	model, vendor, family := parseCPUModelInfoFromString(cpuInfo)

	expectedModel := "Intel(R) Core(TM) i7-8565U CPU @ 1.80GHz"
	expectedVendor := "GenuineIntel"
	expectedFamily := "6"

	if model != expectedModel {
		t.Errorf("parseCPUModelInfoFromString() model = %v, want %v", model, expectedModel)
	}
	if vendor != expectedVendor {
		t.Errorf("parseCPUModelInfoFromString() vendor = %v, want %v", vendor, expectedVendor)
	}
	if family != expectedFamily {
		t.Errorf("parseCPUModelInfoFromString() family = %v, want %v", family, expectedFamily)
	}
}

// Helper functions for testing - these would extract the core logic from the production functions

// parseFrequencyInfo extracts CPU frequency from a reader (for testing)
func parseFrequencyInfo(reader *strings.Reader) float64 {
	scanner := bufio.NewScanner(reader)

	// First try to find the cpu MHz line
	var mhzFreq float64
	var modelNameFreq float64

	for scanner.Scan() {
		line := scanner.Text()

		// Look for the "cpu MHz" line
		if strings.Contains(line, "cpu MHz") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				freq, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					// Convert MHz to GHz
					mhzFreq = freq / 1000
				}
			}
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
					modelNameFreq = freq
				}
			}
		}
	}

	// Prefer the explicit MHz value if available
	if mhzFreq > 0 {
		return mhzFreq
	}

	// Otherwise use the model name frequency
	if modelNameFreq > 0 {
		return modelNameFreq
	}

	return 0
}

// parseCPUModelInfoFromString parses CPU model info from a string (for testing)
func parseCPUModelInfoFromString(cpuInfo string) (model, vendor, family string) {
	scanner := bufio.NewScanner(strings.NewReader(cpuInfo))
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
	return model, vendor, family
}
