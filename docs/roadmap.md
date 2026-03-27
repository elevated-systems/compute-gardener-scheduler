# Compute Gardener Project Roadmap

*Last updated: March 2026*

## Completed

### v0.1.6 - v0.1.7 (March 2025)
- ✅ Hardware profiler for accurate power estimation (CPU model, frequency, DCGM GPU monitoring)
- ✅ Energy budget tracking with configurable actions
- ✅ Grafana dashboard for scheduler metrics and savings visualization
- ✅ Unit test coverage >40%

### v0.2.0 - v0.2.1 (April-May 2025)
- ✅ Validated savings calculations and metrics on GCP (non-autopilot)
- ✅ Kepler data integration support
- ✅ Cascading node detection (custom annotations, NFD labels)

### v0.2.2 (Summer-Fall 2025)
- ✅ AWS validation (non-auto-provisioned nodes)
- ✅ Documentation improvements and blog series on carbon-aware ML training

### v0.2.3 (Q1 2026)
- ✅ **Dry-run mode**: webhook-based "try before you buy" evaluation without scheduler installation
  - SchedulerName mutation: webhook rewrites to default-scheduler for seamless transition
  - Shared evaluation logic: common evaluator used by both scheduler plugin and dry-run webhook
  - Dedicated Grafana dashboard (`dashboards/compute-gardener-dryrun-dashboard.json`)
- ✅ **Savings calculation fixes**: corrected carbon savings to use bind-time intensity, fixed GPU power misattribution in 1:1 GPU-to-node configurations, reworked initial carbon/price metrics
- ✅ **CloudInfo integration**: cloud provider and region detection via CloudInfo, replacing deprecated manual region mapping

## In Progress

### H1 2026

The tail end of 2025 focused on marketing, documentation and building awareness around carbon-aware computing. Ongoing development:

- 🚀 **Simple forecasting**: Schedule at predicted optimal times rather than just waiting for threshold crossings.

- 🚀 **Compute Gardener API**: Integration with our upcoming API service that will provide:
  - Electricity Maps API-like responses of intensity data blended with costs
  - Cloud spot pricing data for cost-optimized scheduling
  - Blended optimization support (e.g., 60% carbon weight / 40% cost weight)

## Future Roadmap

### H2 2026 and Beyond

- 🔮 **Multi-dimensional optimization**: Combined carbon + cost + spot pricing scheduling decisions
- 🔮 **Multi-cluster/spatial deferral**: Route workloads to lower-carbon regions
- 🔮 **Additional carbon data sources**: WattTime and other providers
- 🔮 **Predictive workload classification**: Automatic energy optimization based on workload patterns

## Ongoing
- 📈 Documentation and examples
- 🔍 Benchmarking energy savings across workload types
- 🌱 Building community around carbon-aware computing

----

We welcome contributions! See our [README](../README.md) and [Contributing Guide](../CONTRIBUTING.md) to get involved.