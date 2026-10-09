#!/usr/bin/env bash
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }
KREL=$(uname -r)
MODULE_DIR="/lib/modules/$KREL/extra/ax210"
FIRMWARE='/lib/firmware/iwlwifi-ty-a0-gf-a0-59.ucode'
STATE_DIR='/var/lib/ax210-repair'
BACKUP_DIR=''
[[ -r "$STATE_DIR/backup-dir" ]] && BACKUP_DIR=$(cat "$STATE_DIR/backup-dir")

# Use rmmod for only our modules; modprobe -r could also try to remove
# cfg80211/mac80211, which are shared with other wireless drivers on this board.
if lsmod | grep -q '^iwlmvm\b'; then rmmod iwlmvm; fi
if lsmod | grep -q '^iwlwifi\b'; then rmmod iwlwifi; fi

restore_or_remove() {
    local target=$1 name=$2 backup=${BACKUP_DIR:+$BACKUP_DIR/$2}
    if [[ -n "$BACKUP_DIR" && -e "$backup" ]]; then
        install -D -m 0644 "$backup" "$target"
    else
        rm -f "$target"
    fi
}
restore_or_remove "$MODULE_DIR/iwlwifi.ko" iwlwifi.ko
restore_or_remove "$MODULE_DIR/iwlmvm.ko" iwlmvm.ko
restore_or_remove "$FIRMWARE" iwlwifi-ty-a0-gf-a0-59.ucode
rmdir "$MODULE_DIR" 2>/dev/null || true
depmod -a "$KREL"
rm -f "$STATE_DIR/backup-dir"
echo "AX210 repair modules/firmware removed for $KREL"
