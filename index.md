 # Compute Gardener Scheduler

  A Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity data and time-of-use
  electricity pricing.

  ## Installation

  ```bash
  # Add the Helm repository
  helm repo add compute-gardener https://elevated-systems.github.io/compute-gardener-scheduler
  helm repo update

  # Install the chart
  helm install compute-gardener-scheduler compute-gardener/compute-gardener-scheduler \
    --namespace kube-system \
    --set carbonAware.electricityMap.apiKey=YOUR_API_KEY

  Documentation

  For full documentation, please visit the https://github.com/elevated-systems/compute-gardener-scheduler.
