#!/usr/bin/env bash
set -euo pipefail
model=$(realpath "${1:?Usage: $0 model.onnx}")
name=$(basename "$model" .onnx)
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
out="$root/artifacts/$name"

if ! command -v atc >/dev/null 2>&1; then
    source /usr/local/Ascend/ascend-toolkit/set_env.sh
fi
command -v atc >/dev/null
mkdir -p "$out"
atc --model="$model" --framework=5 --output="$out/$name" --input_format=NCHW --input_shape="images:1,3,640,640" --soc_version=Ascend310B4 --log=error
test -s "$out/$name.om"
sha256sum "$model" "$out/$name.om" > "$out/SHA256SUMS"
printf 'OM: %s\n' "$out/$name.om"
cat "$out/SHA256SUMS"
