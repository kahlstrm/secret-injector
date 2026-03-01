# Release Runbook

## Release contract

- Versioning: SemVer tags in the form `vX.Y.Z`.
- Prereleases: use SemVer prerelease tags (for example `v0.2.0-rc.1`).
- Binary artifacts: `linux` and `darwin` on `amd64` and `arm64`.
- Build mode: release binaries are built with `CGO_ENABLED=0`.
- Container artifacts: GHCR multi-arch image for `linux/amd64` and `linux/arm64`.

## Pre-release checklist

1. Ensure `main` is green in CI.
2. Pull latest `main` locally.
3. Confirm release notes scope from commits since last tag.
4. Pick the next SemVer version.

## Cut a release

```sh
git checkout main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

Pushing the tag triggers `.github/workflows/release.yml` which:

- reruns format, vet, lint, unit tests, and integration tests,
- creates GitHub release binaries and checksums,
- publishes GHCR multi-arch images.

## Post-release verification

1. Verify release assets exist in GitHub Releases.
2. Verify `checksums.txt` is attached.
3. Verify container image exists:

```sh
docker pull ghcr.io/kahlstrm/secret-injector:v0.1.0
docker run --rm ghcr.io/kahlstrm/secret-injector:v0.1.0 --version
```

4. Verify copy-from flow with a local Docker build.

## If release fails

1. Fix the issue on `main`.
2. Create and push a new patch tag (for example `v0.1.1`).
3. Do not overwrite or retag an existing released version.
