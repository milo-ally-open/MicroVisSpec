#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=$(cd -- "$SCRIPT_DIR/.." && pwd)
REPO_ROOT=$(cd -- "$PROJECT_DIR/../../.." && pwd)
ARTIFACTS="$PROJECT_DIR/artifacts"
SDK_ZIP=${SDK_ZIP:-"$REPO_ROOT/readme/Ascend-hdk-310b-sdk-soc_23.0.rc3.zip"}
ROOT_DIR=${AX210_ROOT:-"$HOME/atlas200idk-a2-ax210"}
SDK_DIR="$ROOT_DIR/sdk"
BUILD_ROOT="$ROOT_DIR/build"
KERNEL_OUT="$BUILD_ROOT/linux-5.10.0"
SROOT="$SDK_DIR/Ascend310B-source"
KERNEL_SRC="$SROOT/kernel/kernel/kernel"
TOOLCHAIN="$SDK_DIR/toolchain"
CROSS_COMPILE="$TOOLCHAIN/bin/aarch64-target-linux-gnu-"
CONFIG_GZ="$ARTIFACTS/ax210-board-config-5.10.0+.config.gz"

for tool in python3 tar make gzip; do
    command -v "$tool" >/dev/null || { echo "missing build host tool: $tool" >&2; exit 1; }
done
[[ -f "$SDK_ZIP" ]] || { echo "SDK ZIP not found: $SDK_ZIP" >&2; exit 1; }
[[ -s "$CONFIG_GZ" ]] || { echo "board config missing: $CONFIG_GZ" >&2; exit 1; }
[[ ! -e "$KERNEL_OUT" ]] || { echo "build tree already exists: $KERNEL_OUT (choose a new AX210_ROOT)" >&2; exit 1; }

mkdir -p "$SDK_DIR" "$BUILD_ROOT"
python3 - "$SDK_ZIP" "$SDK_DIR" <<'PY'
import pathlib, shutil, sys, zipfile
archive, destination = sys.argv[1:]
destination = pathlib.Path(destination)
members = {
    "Ascend310B-sdk/Ascend310B-source.tar.gz": "Ascend310B-source.tar.gz",
    "Ascend310B-sdk/toolchain.tar.gz": "toolchain.tar.gz",
}
with zipfile.ZipFile(archive) as sdk:
    for member, filename in members.items():
        target = destination / filename
        with sdk.open(member) as source, target.open("wb") as output:
            shutil.copyfileobj(source, output)
        print(f"extracted SDK component: {filename}")
PY

[[ -d "$SROOT" ]] || tar -xzf "$SDK_DIR/Ascend310B-source.tar.gz" -C "$SDK_DIR"
[[ -x "$CROSS_COMPILE"gcc ]] || tar -xzf "$SDK_DIR/toolchain.tar.gz" -C "$SDK_DIR"
[[ -x "$CROSS_COMPILE"gcc ]] || { echo "Huawei AArch64 cross compiler missing" >&2; exit 1; }

# The Huawei source tree uses relative links for these two overlay trees.
LIBC_SEC="$SROOT/kernel/libc_sec"
[[ -d "$LIBC_SEC/src" ]] || { echo "SDK libc_sec overlay missing: $LIBC_SEC" >&2; exit 1; }
if [[ ! -e "$ROOT_DIR/libc_sec" ]]; then
    ln -s "$LIBC_SEC" "$ROOT_DIR/libc_sec"
fi

top_dir="$BUILD_ROOT" \
kernel_source_dir="$KERNEL_SRC" \
kernel_out_dir="$KERNEL_OUT" \
is_local=1 \
bash "$KERNEL_SRC/kbuild/mksource.sh" > "$BUILD_ROOT/source-prepare.log" 2>&1
cp -a "$KERNEL_SRC/spmi_hisi" "$BUILD_ROOT/spmi_hisi"

PATCH="$PROJECT_DIR/patches/0001-disable-iwlwifi-leds.patch"
git -C "$KERNEL_OUT" apply --check "$PATCH"
git -C "$KERNEL_OUT" apply "$PATCH"
gzip -dc "$CONFIG_GZ" > "$KERNEL_OUT/.config"
"$KERNEL_OUT/scripts/config" --file "$KERNEL_OUT/.config" \
    --enable WLAN_VENDOR_INTEL \
    --module CFG80211 \
    --module MAC80211 \
    --module IWLWIFI \
    --module IWLMVM \
    --disable MAC80211_LEDS

make -s -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" olddefconfig
make -s -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" modules_prepare
[[ $(make -s -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" kernelrelease) == "5.10.0+" ]] || {
    echo "kernelrelease does not match board's 5.10.0+" >&2; exit 1;
}
grep -q '^CONFIG_IWLWIFI=m$' "$KERNEL_OUT/.config"
grep -q '^CONFIG_IWLMVM=m$' "$KERNEL_OUT/.config"
! grep -q 'CONFIG_IWLWIFI_LEDS' "$KERNEL_OUT/include/generated/autoconf.h"

JOBS=${JOBS:-8}
make -s -j"$JOBS" -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" M=net/wireless modules
make -s -j"$JOBS" -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" \
    KBUILD_EXTRA_SYMBOLS="$KERNEL_OUT/net/wireless/Module.symvers" M=net/mac80211 modules
make -s -j"$JOBS" -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" \
    KBUILD_EXTRA_SYMBOLS="$KERNEL_OUT/net/wireless/Module.symvers $KERNEL_OUT/net/mac80211/Module.symvers" \
    M=drivers/net/wireless/intel/iwlwifi modules

cp "$KERNEL_OUT/drivers/net/wireless/intel/iwlwifi/iwlwifi.ko" "$ARTIFACTS/iwlwifi.ko"
cp "$KERNEL_OUT/drivers/net/wireless/intel/iwlwifi/mvm/iwlmvm.ko" "$ARTIFACTS/iwlmvm.ko"
for module in "$ARTIFACTS/iwlwifi.ko" "$ARTIFACTS/iwlmvm.ko"; do
    vermagic=$(modinfo -F vermagic "$module")
    [[ "$vermagic" == 5.10.0+\ SMP\ mod_unload\ aarch64* ]] || {
        echo "unexpected vermagic: $module: $vermagic" >&2; exit 1;
    }
done
if nm -u "$ARTIFACTS/iwlmvm.ko" | grep -q '__ieee80211_get_radio_led_name'; then
    echo "iwlmvm still references the unavailable optional LED symbol" >&2
    exit 1
fi
sha256sum "$ARTIFACTS/iwlwifi.ko" "$ARTIFACTS/iwlmvm.ko"
echo "Built ARM64 AX210 modules for $(make -s -C "$KERNEL_OUT" ARCH=arm64 CROSS_COMPILE="$CROSS_COMPILE" kernelrelease)"
