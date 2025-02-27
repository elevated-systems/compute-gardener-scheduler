# Contributing to Compute Gardener Scheduler

Welcome! We're excited about your interest in contributing to the Compute Gardener Scheduler. This project, while forked from the Kubernetes scheduler-plugins repository, maintains its own development focus on carbon and price-aware scheduling.

## Code of Conduct

As a Kubernetes-adjacent project, we follow the CNCF [code of conduct](code-of-conduct.md). We are committed to fostering an open and welcoming community.

## Getting Started

1. Fork the [compute-gardener-scheduler repository](https://github.com/elevated-systems/compute-gardener-scheduler)
2. Clone your fork and create a new branch for your contribution
3. Make your changes, following our coding conventions and practices
4. Write or update tests as needed
5. Submit a pull request

## Development Environment

To set up your development environment:

1. Install Go (see go.mod for version requirements)
2. Install required development tools:
   ```bash
   make install-tools
   ```
3. Run tests to verify your setup:
   ```bash
   make test
   ```

## Pull Request Process

1. Ensure your code follows our formatting standards:
   ```bash
   make verify-gofmt
   ```
2. Update documentation as needed
3. Add tests for new functionality
4. Ensure all tests pass:
   ```bash
   make test
   ```
5. Update the README.md with details of significant changes

## Contribution Areas

We welcome contributions in several areas:

- Core scheduling logic improvements
- Carbon awareness features
- Price-aware scheduling enhancements
- Documentation improvements
- Bug fixes
- Test coverage

## Contact

For questions or discussions:
- Open an issue in the repository
- Contact the maintainers directly

## License

By contributing to this project, you agree to license your contributions under the same license as the project (Apache 2.0).
