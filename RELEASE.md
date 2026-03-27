# Release Guide

This document describes the process for creating new releases of the Compute Gardener Scheduler.

## Release Process

1. Ensure your local repository is clean and up-to-date
   ```bash
   git checkout main
   git pull origin main
   ```

2. Run all tests and verify they pass
   ```bash
   make unit-test
   ```

3. Create and push a lightweight git tag
   ```bash
   VERSION=v0.2.3  # Update version as appropriate
   # IMPORTANT: Use a lightweight tag (no -a or -m flags).
   # Annotated tags (created with -a or -m) break `git tag --sort=-committerdate`
   # used in the Makefile because they have a "tagger date" instead of "committer date".
   git tag $VERSION
   git push origin $VERSION
   ```

4. Build and push the container image(s) to Docker Hub
   ```bash
   make build-push-images
   ```

5. Update the default image tags in `values.yaml` to point to the new release images

6. Update `manifests/install/charts/compute-gardener-scheduler/Chart.yaml`:
   - Set `version` to the new chart version (increment when chart/values change)
   - Set `appVersion` to match the new tag + short commit hash, e.g. `"v0.2.3-abc1234"`

   Commit and push to `main`. This automatically triggers the
   `.github/workflows/release-charts.yml` workflow, which uses
   [chart-releaser](https://github.com/helm/chart-releaser-action) to create
   a GitHub Release from the new chart version.

## Version Numbering

We follow [Semantic Versioning](https://semver.org/):
- MAJOR version for incompatible API changes
- MINOR version for new functionality in a backwards compatible manner
- PATCH version for backwards compatible bug fixes

Pre-release versions use the `-alpha` suffix (e.g. `v0.2.3-alpha`).

## Key Files

| File | What to update |
|------|---------------|
| `manifests/install/charts/compute-gardener-scheduler/Chart.yaml` | `version` and `appVersion` — triggers GitHub Release via CI |
| `manifests/install/charts/compute-gardener-scheduler/values.yaml` | Default `image` tags for scheduler and dryrun |
| `Makefile` | `VERSION` if it references a hardcoded version |

## Docker Images

Images are published to Docker Hub:
- `docker.io/dmasselink/compute-gardener-scheduler:${VERSION}`
- `docker.io/dmasselink/compute-gardener-dryrun:${VERSION}` (dry-run webhook)

## Post-Release
- Verify the GitHub Release was created by the chart-releaser workflow
- Update documentation if needed
- Announce the release in relevant channels
