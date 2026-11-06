#!/bin/bash
# End-to-end test in a clean container: test/e2e/run.sh [image ...] (default ubuntu:24.04).
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cp -r "$root/test/e2e/fixture" "$root/test/e2e/check.sh" "$root/test/e2e/bitwarden.sh" "$root/test/e2e/dconf.sh" "$root/test/e2e/wsl.sh" "$root/test/e2e/harness.sh" "$root/test/e2e/sessions.sh" "$root/test/e2e/autocapture.sh" \
  "$root/test/e2e/fixture-v1" "$root/test/e2e/migrate.sh" "$root/test/e2e/nodes.sh" "$work/"
(cd "$root" && CGO_ENABLED=0 go build -o "$work/rehome" ./cmd/rehome)
# apt reads the local fixture repo as the _apt user.
chmod -R a+rX "$work"

status=0

for image in "${@:-ubuntu:24.04}"; do
  echo "### $image"

  if ! docker run --rm -v "$work:/e2e:ro" "$image" bash /e2e/check.sh; then
    status=1
  fi
done

exit "$status"
