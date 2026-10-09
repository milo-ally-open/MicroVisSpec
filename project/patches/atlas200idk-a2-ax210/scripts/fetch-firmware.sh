#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=$(cd -- "$SCRIPT_DIR/.." && pwd)
ARTIFACTS="$PROJECT_DIR/artifacts"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

URL='https://archive.ubuntu.com/ubuntu/pool/main/l/linux-firmware-intel-wireless/linux-firmware-intel-wireless_20240318.git3b128b60-0ubuntu3.1_all.deb'
PACKAGE="$WORK/linux-firmware-intel-wireless.deb"
EXTRACTED="$WORK/root"
TARGET="$ARTIFACTS/iwlwifi-ty-a0-gf-a0-59.ucode"

for tool in curl dpkg-deb zstd; do
    command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done
mkdir -p "$ARTIFACTS" "$EXTRACTED"
curl --fail --location --retry 3 "$URL" --output "$PACKAGE"
dpkg-deb -x "$PACKAGE" "$EXTRACTED"
SOURCE="$EXTRACTED/lib/firmware/intel/iwlwifi/iwlwifi-ty-a0-gf-a0-59.ucode.zst"
[[ -s "$SOURCE" ]] || { echo "API 59 AX210 firmware missing from package" >&2; exit 1; }
zstd --decompress --stdout "$SOURCE" > "$TARGET"
echo '7620861bcd60e5e89a2f0571b98e67f9451c52ce6c912b46c2f141b0005edd6c  '"$TARGET" | sha256sum --check
