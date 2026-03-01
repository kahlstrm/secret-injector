# Release Runbook

## Release contract

- Versioning: SemVer tags in the form `vX.Y.Z`.
- Prereleases: use SemVer prerelease tags (for example `v0.2.0-rc.1`).
- Binary artifacts: `linux` and `darwin` on `amd64` and `arm64`.
- Build mode: release binaries are built with `CGO_ENABLED=0`.
- Container artifacts: GHCR multi-arch image for `linux/amd64` and `linux/arm64`.
- GitHub Releases: created as drafts and published after review.

## Pre-release checklist

1. Ensure `main` is green in CI.
2. Pull latest `main` locally.
3. Confirm release notes scope from commits since last tag.
4. Pick the next SemVer version.
5. Optional local preflight: `make release-dry-run`.

## Cut a release

Create and push a SemVer tag from `main`:

```sh
git checkout main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

Tag pushes trigger `.github/workflows/release.yml`, which:

- validates the tag format as SemVer,
- reruns format, vet, lint, unit tests, and integration tests,
- creates release binaries and `checksums.txt`,
- publishes GHCR multi-arch images,
- creates/updates a draft GitHub Release.

## Publish the draft release

1. Open the draft in GitHub Releases.
2. Verify release notes and attached artifacts.
3. Click **Publish release**.

## Post-release verification

1. Verify the published release assets exist in GitHub Releases.
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
