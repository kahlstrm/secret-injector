#!/bin/sh

set -eu

dist_dir=dist
artifacts_file="$dist_dir/artifacts.json"
metadata_file="$dist_dir/metadata.json"
expected_archives='[["darwin","amd64"],["darwin","arm64"],["linux","amd64"],["linux","arm64"]]'
expected_images='[["linux","amd64"],["linux","arm64"]]'

for command in cmp docker git jq mktemp tar; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "required command not found: $command" >&2
		exit 1
	fi
done

for file in "$artifacts_file" "$metadata_file" "$dist_dir/checksums.txt"; do
	if [ ! -f "$file" ]; then
		echo "release artifact not found: $file" >&2
		exit 1
	fi
done

version=$(jq -er '.version | select(length > 0)' "$metadata_file")
commit=$(jq -er '.commit | select(length > 0)' "$metadata_file")
head_commit=$(git rev-parse HEAD)
if [ "$commit" != "$head_commit" ]; then
	echo "release artifacts were built from $commit, expected $head_commit" >&2
	exit 1
fi

archive_targets=$(jq -c '[.[] | select(.type == "Archive") | [.goos, .goarch]] | sort' "$artifacts_file")
if [ "$archive_targets" != "$expected_archives" ]; then
	echo "unexpected archive targets: $archive_targets" >&2
	exit 1
fi

image_targets=$(jq -c '[.[] | select(.type == "Docker Image") | [.goos, .goarch]] | sort' "$artifacts_file")
if [ "$image_targets" != "$expected_images" ]; then
	echo "unexpected Docker image targets: $image_targets" >&2
	exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
	(cd "$dist_dir" && sha256sum -c checksums.txt)
elif command -v shasum >/dev/null 2>&1; then
	(cd "$dist_dir" && shasum -a 256 -c checksums.txt)
else
	echo "required checksum command not found: sha256sum or shasum" >&2
	exit 1
fi

for archive in $(jq -r '.[] | select(.type == "Archive") | .path' "$artifacts_file"); do
	if [ ! -f "$archive" ]; then
		echo "archive not found: $archive" >&2
		exit 1
	fi

	contents=$(tar -tzf "$archive" | LC_ALL=C sort)
	if [ "$contents" != "LICENSE
README.md
secret-injector" ]; then
		echo "unexpected contents in $archive:" >&2
		echo "$contents" >&2
		exit 1
	fi

	goos=$(jq -r --arg path "$archive" '.[] | select(.type == "Archive" and .path == $path) | .goos' "$artifacts_file")
	goarch=$(jq -r --arg path "$archive" '.[] | select(.type == "Archive" and .path == $path) | .goarch' "$artifacts_file")
	binary=$(jq -r --arg goos "$goos" --arg goarch "$goarch" '.[] | select(.type == "Binary" and .goos == $goos and .goarch == $goarch) | .path' "$artifacts_file")
	if [ ! -f "$binary" ]; then
		echo "binary not found for $goos/$goarch: $binary" >&2
		exit 1
	fi
	tar -xOzf "$archive" secret-injector | cmp - "$binary"
done

for arch in amd64 arm64; do
	image=$(jq -r --arg arch "$arch" '.[] | select(.type == "Docker Image" and .goarch == $arch) | .path' "$artifacts_file")
	if [ -z "$image" ] || [ "$image" = "null" ]; then
		echo "Docker image not found for linux/$arch" >&2
		exit 1
	fi

	image_os=$(docker image inspect "$image" --format '{{.Os}}')
	image_arch=$(docker image inspect "$image" --format '{{.Architecture}}')
	image_version=$(docker image inspect "$image" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')
	image_commit=$(docker image inspect "$image" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')
	image_date=$(docker image inspect "$image" --format '{{index .Config.Labels "org.opencontainers.image.created"}}')

	if [ "$image_os/$image_arch" != "linux/$arch" ]; then
		echo "unexpected image platform for $image: $image_os/$image_arch" >&2
		exit 1
	fi
	if [ "$image_version" != "$version" ] || [ "$image_commit" != "$commit" ]; then
		echo "unexpected image metadata for $image" >&2
		exit 1
	fi

	expected_output="secret-injector version $version (commit=$commit date=$image_date)"
	actual_output=$(docker run --rm --platform "linux/$arch" "$image" --version)
	if [ "$actual_output" != "$expected_output" ]; then
		echo "unexpected --version output for $image: $actual_output" >&2
		exit 1
	fi
done

amd64_image=$(jq -r '.[] | select(.type == "Docker Image" and .goarch == "amd64") | .path' "$artifacts_file")
copy_image="secret-injector-release-copy-test:$version"
build_context=
cleanup() {
	docker image rm -f "$copy_image" >/dev/null 2>&1 || true
	if [ -n "$build_context" ]; then
		rm -rf "$build_context"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
build_context=$(mktemp -d)
daemon_builder=$(docker context show)
docker buildx build --builder "$daemon_builder" --load --platform linux/amd64 --build-arg "SOURCE_IMAGE=$amd64_image" -t "$copy_image" -f - "$build_context" <<'EOF'
ARG SOURCE_IMAGE=scratch
FROM ${SOURCE_IMAGE} AS source
FROM scratch
COPY --from=source /secret-injector /secret-injector
ENTRYPOINT ["/secret-injector"]
EOF

copy_output=$(docker run --rm --platform linux/amd64 "$copy_image" --version)
source_output=$(docker run --rm --platform linux/amd64 "$amd64_image" --version)
if [ "$copy_output" != "$source_output" ]; then
	echo "COPY --from image output does not match source image" >&2
	exit 1
fi

echo "Verified GoReleaser artifacts for $version"
