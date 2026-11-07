#!/bin/sh
set -eu
image=$1 recipe=$2 controls=$3 output=$4
image_id=$(docker image inspect --format '{{.Id}}' "$image")
case "$image_id" in sha256:*) ;; *) echo 'missing image identity' >&2; exit 1;; esac
fixture=$(mktemp -d)
cid=
cleanup() {
    if [ -n "$cid" ]; then docker rm -v "$cid" >/dev/null; fi
    rm -rf -- "$fixture"
}
trap cleanup EXIT HUP INT TERM
cid=$(docker create --read-only "$image_id")
docker export "$cid" > "$fixture/rootfs.tar"
# No LD_PRELOAD or library path override: this runs against the final loader/libc.
docker run --rm --pull never --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --user 10001:10001 --pids-limit 32 --memory 128m \
  --volume "$controls:/proof:ro" --entrypoint /proof/glibc-witness "$image_id" /proof patched
python3 scripts/verify-runtime-remediation.py --rootfs "$fixture/rootfs.tar" --control "$controls" \
  --image-id "$image_id" --recipe "$recipe" --policy security/runtime-remediation.json > "$output"
