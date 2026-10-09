#!/usr/bin/env bash
set -euo pipefail

ARTIFACTS=${1:-"$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../artifacts" && pwd)"}
KREL=$(uname -r)
EXPECTED='5.10.0+'
[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }
[[ "$KREL" == "$EXPECTED" ]] || { echo "refusing kernel $KREL; artifacts target $EXPECTED" >&2; exit 1; }

for file in iwlwifi.ko iwlmvm.ko iwlwifi-ty-a0-gf-a0-59.ucode; do
    [[ -s "$ARTIFACTS/$file" ]] || { echo "missing artifact: $ARTIFACTS/$file" >&2; exit 1; }
done
for module in iwlwifi.ko iwlmvm.ko; do
    vermagic=$(modinfo -F vermagic "$ARTIFACTS/$module")
    [[ "$vermagic" == 5.10.0+\ SMP\ mod_unload\ aarch64* ]] || {
        echo "wrong module vermagic: $module: $vermagic" >&2; exit 1;
    }
done

if ! command -v iw >/dev/null && [[ -f "$ARTIFACTS/iw_5.16-1build1_arm64.deb" ]]; then
    dpkg -i "$ARTIFACTS/iw_5.16-1build1_arm64.deb"
fi
if ! command -v rfkill >/dev/null && [[ -f "$ARTIFACTS/rfkill_2.37.2-4ubuntu3_arm64.deb" ]]; then
    dpkg -i "$ARTIFACTS/rfkill_2.37.2-4ubuntu3_arm64.deb"
fi

MODULE_DIR="/lib/modules/$KREL/extra/ax210"
FIRMWARE='/lib/firmware/iwlwifi-ty-a0-gf-a0-59.ucode'
STATE_DIR='/var/lib/ax210-repair'
BACKUP_DIR="/var/backups/ax210-repair/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$STATE_DIR"
save_if_present() {
    local target=$1 name=$2
    if [[ -e "$target" ]]; then
        mkdir -p "$BACKUP_DIR"
        cp -a "$target" "$BACKUP_DIR/$name"
    fi
}
save_if_present "$MODULE_DIR/iwlwifi.ko" iwlwifi.ko
save_if_present "$MODULE_DIR/iwlmvm.ko" iwlmvm.ko
save_if_present "$FIRMWARE" iwlwifi-ty-a0-gf-a0-59.ucode

# Unload only this driver's modules before replacing files. Do not let
# modprobe -r cascade into cfg80211/mac80211, which other NIC drivers use.
if lsmod | grep -q '^iwlmvm\b'; then rmmod iwlmvm; fi
if lsmod | grep -q '^iwlwifi\b'; then rmmod iwlwifi; fi

if [[ -d "$BACKUP_DIR" ]]; then
    printf '%s\n' "$BACKUP_DIR" > "$STATE_DIR/backup-dir"
else
    rm -f "$STATE_DIR/backup-dir"
fi

install -D -m 0644 "$ARTIFACTS/iwlwifi.ko" "$MODULE_DIR/iwlwifi.ko"
install -D -m 0644 "$ARTIFACTS/iwlmvm.ko" "$MODULE_DIR/iwlmvm.ko"
install -m 0644 "$ARTIFACTS/iwlwifi-ty-a0-gf-a0-59.ucode" "$FIRMWARE"
depmod -a "$KREL"
rfkill unblock wifi
modprobe iwlmvm
IFACE=$(iw dev | awk '$1 == "Interface" {print $2; exit}')
[[ -n "$IFACE" ]] || { echo "driver loaded but no wireless interface appeared" >&2; exit 1; }
ip link set "$IFACE" up
echo "AX210 ready: interface=$IFACE kernel=$KREL"
