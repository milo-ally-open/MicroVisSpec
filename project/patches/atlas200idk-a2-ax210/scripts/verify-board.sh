#!/usr/bin/env bash
set -euo pipefail

echo "Kernel: $(uname -r) ($(uname -m))"
lspci -nnk -s 0001:09:00.0
modinfo iwlwifi | grep -E '^(filename|version|firmware|depends|vermagic):' | head -12
lsmod | grep -E '^(iwlwifi|iwlmvm|cfg80211|mac80211)\b'
rfkill list wifi
IFACE=$(iw dev | awk '$1 == "Interface" {print $2; exit}')
[[ -n "$IFACE" ]] || { echo "No wireless interface" >&2; exit 1; }
ip -br link show "$IFACE"
bash "$(dirname -- "${BASH_SOURCE[0]}")/scan.sh" "$IFACE"
