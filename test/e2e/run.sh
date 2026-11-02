#!/bin/bash
# End-to-end test in a clean container: test/e2e/run.sh [image ...] (default ubuntu:24.04).
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cp -r "$root/test/e2e/fixture" "$root/test/e2e/check.sh" "$work/"
(cd "$root" && CGO_ENABLED=0 go build -o "$work/rehome" ./cmd/rehome)

status=0

for image in "${@:-ubuntu:24.04}"; do
  echo "### $image"

  if ! docker run --rm -v "$work:/e2e:ro" "$image" bash /e2e/check.sh; then
    status=1
  fi
done

exit "$status"
