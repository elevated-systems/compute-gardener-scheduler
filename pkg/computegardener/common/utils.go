package common

import (
	v1 "k8s.io/api/core/v1"
)

// IsGPUPod determines if a pod requires GPU resources by checking for
// nvidia runtime and GPU resource requests
func IsGPUPod(pod *v1.Pod) bool {
	// Check for nvidia runtime class
	if pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "nvidia" {
		return false
	}

	// Check for GPU resource requests
	for _, container := range pod.Spec.Containers {
		if val, exists := container.Resources.Requests["nvidia.com/gpu"]; exists && !val.IsZero() {
			return true
		}
	}

	return false
}