# Compute Gardener Project Roadmap

*Last updated: January 2026*

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
- ✅ Dry-run mode: webhook-based evaluation without scheduler installation
- ✅ AWS validation (non-auto-provisioned nodes)
- ✅ Documentation improvements and blog series on carbon-aware ML training

## In Progress

### H1 2026

The tail end of 2025 focused on marketing, documentation and building awareness around carbon-aware computing. Ongoing development:

- 🚀 **Dry-run mode polish**: Completing the dry-run admission webhook for production readiness. Allows evaluation of scheduling decisions and savings potential without installing a secondary scheduler - reducing adoption risk for curious teams.

- 🚀 **Simple forecasting**: Schedule at predicted optimal times rather than just waiting for threshold crossings.

- 🚀 **Compute Gardener API**: Integration with our upcoming API service that will provide:
  - Electricity Maps API pass-through (familiar interface, additional features)
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