#!/usr/bin/env bash

# Renders raw "kubectl apply"-friendly manifests from the Helm chart.
# Three blessed variants are produced:
#   - compute-gardener-scheduler.yaml          (standard install with metrics)
#   - compute-gardener-scheduler-no-metrics.yaml (minimal install, no Prometheus deps)
#   - compute-gardener-scheduler-dryrun.yaml   (dry-run admission webhook mode)
# The shared hardware-profiles ConfigMap is split into:
#   - compute-gardener-scheduler-hw-profiles.yaml
#
# The chart is the single source of truth; rerun this script after any chart change.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR_REL="manifests/install/charts/compute-gardener-scheduler"
CHART_DIR="${REPO_ROOT}/${CHART_DIR_REL}"
OUT_DIR="${REPO_ROOT}/manifests/compute-gardener-scheduler"
RELEASE_NAME="compute-gardener"
RELEASE_NAMESPACE="compute-gardener"

if ! command -v helm >/dev/null 2>&1; then
  echo "error: helm is required to render manifests; install from https://helm.sh" >&2
  exit 1
fi

HEADER_FMT='# Generated from manifests/install/charts/compute-gardener-scheduler.
# DO NOT EDIT — rerun `make manifests` after changing the chart.
# Variant: %s
# Render command: %s
'

render() {
  local out_file="$1"
  local variant="$2"
  shift 2
  local cmd=(helm template "${RELEASE_NAME}" "${CHART_DIR}" --namespace "${RELEASE_NAMESPACE}" "$@")
  # Display version (chart path made relative for portable headers).
  local cmd_display=(helm template "${RELEASE_NAME}" "${CHART_DIR_REL}" --namespace "${RELEASE_NAMESPACE}" "$@")
  local rendered
  rendered="$("${cmd[@]}")"
  {
    # shellcheck disable=SC2059
    printf "${HEADER_FMT}" "${variant}" "${cmd_display[*]}"
    # Helm relies on --create-namespace; for raw kubectl apply, prepend the
    # Namespace doc so a single `kubectl apply -f` works end-to-end.
    cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${RELEASE_NAMESPACE}
EOF
    # Drop the per-document "# Source: ..." chart-path comments — they leak chart
    # internals that aren't useful in raw output.
    echo "${rendered}" | awk '!/^# Source: /'
  } >"${out_file}"
  echo "wrote ${out_file}"
}

# Split the hardware-profiles ConfigMap out of a rendered file into its own
# document, leaving the rest behind. Uses awk to walk YAML doc boundaries.
split_hw_profiles() {
  local in_file="$1"
  local hw_out="$2"
  local rest_tmp
  rest_tmp="$(mktemp)"
  awk -v hw_out="${hw_out}" -v rest_out="${rest_tmp}" '
    BEGIN { buf = ""; out = rest_out }
    /^---[[:space:]]*$/ {
      if (buf != "") print buf > out
      buf = $0
      out = rest_out
      next
    }
    {
      buf = (buf == "" ? $0 : buf "\n" $0)
      # Match only the top-level metadata.name (two-space indent), not the deeply
      # indented configMap volume reference inside the Deployment.
      if ($0 ~ /^  name: compute-gardener-scheduler-hw-profiles[[:space:]]*$/) out = hw_out
    }
    END { if (buf != "") print buf > out }
  ' "${in_file}"
  mv "${rest_tmp}" "${in_file}"
  echo "split hw-profiles -> ${hw_out}"
}

mkdir -p "${OUT_DIR}"

# 1. Standard variant: full install with metrics + ServiceMonitor + GPU exporter,
#    no dry-run, sample pod off (raw users typically don't want a demo pod).
render "${OUT_DIR}/compute-gardener-scheduler.yaml" "standard" \
  --set-string carbonAware.electricityMap.apiKey=YOUR_ELECTRICITY_MAP_API_KEY \
  --set samplePod.enabled=false

# 2. No-metrics variant: skip Service, ServiceMonitor, Prometheus, DCGM exporter.
render "${OUT_DIR}/compute-gardener-scheduler-no-metrics.yaml" "no-metrics" \
  --set-string carbonAware.electricityMap.apiKey=YOUR_ELECTRICITY_MAP_API_KEY \
  --set samplePod.enabled=false \
  --set metrics.enabled=false \
  --set metrics.service.enabled=false \
  --set metrics.serviceMonitor.enabled=false \
  --set metrics.gpuMetrics.enabled=false

# 3. Dry-run variant: webhook admission mode, no scheduler. Uses cert-manager to
#    issue the webhook cert (cert-manager must be installed in the cluster first).
render "${OUT_DIR}/compute-gardener-scheduler-dryrun.yaml" "dry-run" \
  --set-string carbonAware.electricityMap.apiKey=YOUR_ELECTRICITY_MAP_API_KEY \
  --set samplePod.enabled=false \
  --set dryRun.enabled=true

# Hardware profiles ConfigMap is shared by all three variants — split it out so
# users can apply it once per cluster.
split_hw_profiles "${OUT_DIR}/compute-gardener-scheduler.yaml" \
  "${OUT_DIR}/compute-gardener-scheduler-hw-profiles.yaml"
# The other two also embed it; remove the duplicate so each file applies cleanly
# alongside the shared hw-profiles file.
split_hw_profiles "${OUT_DIR}/compute-gardener-scheduler-no-metrics.yaml" /dev/null
split_hw_profiles "${OUT_DIR}/compute-gardener-scheduler-dryrun.yaml" /dev/null

echo "done"
