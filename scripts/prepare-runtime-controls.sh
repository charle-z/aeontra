#!/bin/sh
set -eu
destination=$1
mkdir -p "$destination"
docker buildx build --file Dockerfile.sandbox-workcell --target glibc-backport --load --tag aeontra-runtime-controls:ci .
cid=$(docker create aeontra-runtime-controls:ci)
trap 'docker rm -v "$cid" >/dev/null' EXIT HUP INT TERM
docker cp "$cid:/out/control/." "$destination/"
