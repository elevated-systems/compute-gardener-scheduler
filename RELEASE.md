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
   make test
   ```

3. Create and push a new tag
   ```bash
   VERSION=v0.1.0  # Update version as appropriate
   git tag -m $VERSION $VERSION
   git push origin $VERSION
   ```

4. Build and push the container image to Docker Hub
   ```bash
   # Build the image
   make build-release-image

   # Push to your Docker Hub repository
   # Replace <your-dockerhub-username> with your Docker Hub username
   docker push docker.io/<your-dockerhub-username>/compute-gardener-scheduler:$VERSION
   ```

5. Create a GitHub Release
   - Go to [Releases](https://github.com/elevated-systems/compute-gardener-scheduler/releases)
   - Click "Draft a new release"
   - Choose the tag you just created
   - Title: "Compute Gardener Scheduler $VERSION"
   - Describe the major changes and any breaking changes
   - Include upgrade instructions if necessary
   - List notable bug fixes and new features

## Version Numbering

We follow [Semantic Versioning](https://semver.org/):
- MAJOR version for incompatible API changes
- MINOR version for new functionality in a backwards compatible manner
- PATCH version for backwards compatible bug fixes

## Release Notes Template

```markdown
## Compute Gardener Scheduler ${VERSION}

### Breaking Changes
- List any breaking changes

### New Features
- List new features

### Bug Fixes
- List bug fixes

### Other Improvements
- List other improvements

### Upgrade Instructions
Instructions for upgrading from the previous version...
```

## Docker Images

Images should be published to your Docker Hub repository at:
- `docker.io/<your-dockerhub-username>/compute-gardener-scheduler:${VERSION}`
- `docker.io/<your-dockerhub-username>/compute-gardener-scheduler:latest`

Note: Make sure to update the image reference in your deployment manifests to match your Docker Hub username.

## Post-Release
- Update documentation if needed
- Announce the release in relevant channels
- Create new milestone for next version if appropriate
