#!/usr/bin/env bash
set -euo pipefail

IFACE=${1:-wlan0}
command -v iw >/dev/null || { echo "iw is required" >&2; exit 1; }
rfkill unblock wifi
ip link set "$IFACE" up
iw dev "$IFACE" scan | awk '
    /^BSS / {
        count++
        if (count > 20) exit
        print
        next
    }
    /^[[:space:]]+(freq|signal|SSID):/ { print }
    END { printf "BSS entries shown: %d\n", (count > 20 ? 20 : count) }
'
