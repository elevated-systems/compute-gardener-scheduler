# Compute Gardener Project Roadmap

## Recently Completed

### v0.1.6 (mid March 2025)
- ✅ Integrated hardware profiler for accurate power estimation based on CPU model and frequency
- ✅ Added support for DCGM exporter integration for precise GPU power monitoring
- ✅ Implemented energy budget tracking with configurable actions (log/notify/annotate/label)

### v0.1.7 (late March 2025)
- ✅ This roadmap document
- ✅ Improved hardware detection with dynamic CPU frequency scaling considerations
- ✅ Enhanced monitoring with new metrics for energy efficiency and PUE tracking
- ✅ Grafana dashboard visualizing most scheduler captured metrics including carbon and cost savings
- ✅ Extend unit test coverage to >40%

### v0.2.0 (April 2025)
- ✅ Validate savings calculations and metrics collection
- ✅ Validate as secondary scheduler in Google Cloud Platform (not autopilot nodes)
- ✅ Various metrics collection and dashboard viz enhancements

### v0.2.1 (May 2025)
- ✅ Build initial support for Kepler data integration
- ✅ Support various, cascading node detection strategies: custom annotations, NFD labels
- ✅ Addl dashboard viz enhancements

## Upcoming Releases

### v0.2.2 (June 2025)
- 🚀 Implement simple forecasting to schedule at optimal times (not just waiting for threshold)
- 🚀 Validate as secondary scheduler in AWS (likely not auto-provisioned nodes)
- 🚀 Enhance and validate energy budget admission webhook
- 🚀 Increase test coverage to >60%

### v0.3.0 (mid-late 2025)
- 🔮 Implement multi-cluster/spatial deferral (likely)
- 🔮 Support for custom carbon intensity sources beyond Electricity Maps API (likely WattTime)
- 🔮 Develop predictive workload classification for automatic energy optimization (maybe)

## Ongoing Initiatives
- 📈 Improving documentation and examples
- 🔍 Benchmarking energy savings across different workload types
- 🌱 Building community around carbon-aware computing

----

We welcome community contributions to help us achieve these goals. Please see our [README](../README.md) and [Contributing Guide](../CONTRIBUTING.md) for more information on how to get involved.