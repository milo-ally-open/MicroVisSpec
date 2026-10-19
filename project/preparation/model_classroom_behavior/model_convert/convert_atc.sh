#!/usr/bin/env bash
set -euo pipefail
model=$(realpath "${1:?Usage: $0 model.onnx}")
name=$(basename "$model" .onnx)
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
out="$root/artifacts/$name"
aipp="$root/aipp_nv21.cfg"

if ! command -v atc >/dev/null 2>&1; then
    source /usr/local/Ascend/ascend-toolkit/set_env.sh
fi
if [[ -f /usr/local/miniconda3/etc/profile.d/conda.sh ]]; then
    source /usr/local/miniconda3/etc/profile.d/conda.sh
    conda activate base
    source /usr/local/Ascend/ascend-toolkit/set_env.sh
fi
command -v atc >/dev/null
mkdir -p "$out"
# Atlas development kits have a small memory cgroup; keep operator compilation
# bounded while allowing an explicit override for larger hosts.
export TE_PARALLEL_COMPILER="${TE_PARALLEL_COMPILER:-1}"
atc --model="$model" --framework=5 --output="$out/$name" --input_format=NCHW --input_shape="images:1,3,640,640" --insert_op_conf="$aipp" --soc_version=Ascend310B4 --log=error
test -s "$out/$name.om"
sha256sum "$model" "$aipp" "$out/$name.om" > "$out/SHA256SUMS"
printf 'OM: %s\n' "$out/$name.om"
cat "$out/SHA256SUMS"
