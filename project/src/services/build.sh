#!/usr/bin/env sh
set -eu

services_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
output_dir="$services_dir/bin/linux-amd64"
mkdir -p "$output_dir"

build_service() {
	name=$1
	directory=$2
	printf 'Building %s...\n' "$name"
	(
		cd "$services_dir/$directory/go"
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$output_dir/$name" "./cmd/$name"
	)
}

build_service auth-service auth-service
build_service device-service device-service
build_service event-service event-service
build_service media-service media-service
build_service push-service push-service

printf 'Binaries are ready in %s\n' "$output_dir"
