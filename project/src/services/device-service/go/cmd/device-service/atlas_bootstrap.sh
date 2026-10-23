set -eu
umask 077

device_id=$1
server_origin=$2
work_dir=$3
archive_size=$4
archive_file="$work_dir/atlas200idka2.tar.gz"
source_dir="$work_dir/source"
build_dir="$work_dir/build"

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT HUP INT TERM

case "$work_dir" in
  /tmp/yunshao-bootstrap-????????-????-????-????-????????????) ;;
  *) printf '__YUNSHAO_ERROR:invalid_work_directory__\n'; exit 30 ;;
esac
if [ -e "$work_dir" ]; then
  printf '__YUNSHAO_ERROR:work_directory_exists__\n'
  exit 30
fi
mkdir -m 0700 "$work_dir"
printf '__YUNSHAO_ARCHIVE_INPUT__\n'
head -c "$archive_size" > "$archive_file"
if [ "$(wc -c < "$archive_file")" -ne "$archive_size" ]; then
  printf '__YUNSHAO_ERROR:archive_incomplete__\n'
  exit 31
fi

printf '__YUNSHAO_STAGE:checking_board__\n'
case "$(uname -m)" in
  aarch64|arm64) ;;
  *) printf '__YUNSHAO_ERROR:unsupported_architecture__\n'; exit 32 ;;
esac
if [ ! -r /etc/yunshao/cann.env ] || ! grep -q '^ASCEND_TOOLKIT_HOME=' /etc/yunshao/cann.env; then
  printf '__YUNSHAO_ERROR:cann_environment_missing__\n'
  exit 33
fi
set -a
# This root-owned file is installed as part of the trusted CANN environment.
. /etc/yunshao/cann.env
set +a

tar -xzf "$archive_file" -C "$work_dir" 2>/dev/null || {
  printf '__YUNSHAO_ERROR:archive_invalid__\n'
  exit 34
}
source_dir="$work_dir"

ensure_service_user() {
  if ! getent group yunshao >/dev/null 2>&1; then groupadd --system yunshao; fi
  if ! getent passwd yunshao >/dev/null 2>&1; then
    useradd --system --gid yunshao --home-dir /var/lib/yunshao --no-create-home --shell /usr/sbin/nologin yunshao
  fi
  for group in video HwHiAiUser HwBaseUser; do
    if ! getent group "$group" >/dev/null 2>&1; then
      printf '__YUNSHAO_ERROR:atlas_groups_missing__\n'
      exit 35
    fi
  done
  usermod -a -G video,HwHiAiUser,HwBaseUser yunshao
  if [ -e /var/lib/yunshao ] && [ ! -d /var/lib/yunshao ]; then
    printf '__YUNSHAO_ERROR:agent_state_path_invalid__\n'
    exit 35
  fi
  if [ ! -d /var/lib/yunshao ]; then install -d -m 0750 -o yunshao -g yunshao /var/lib/yunshao; fi
  install -d -m 0750 -o root -g yunshao /etc/yunshao
  for config in "$source_dir"/config/*.json; do
    [ -f "$config" ] || continue
    target="/etc/yunshao/$(basename "$config")"
    if [ ! -e "$target" ]; then install -m 0640 -o root -g yunshao "$config" "$target"; fi
  done
  install -D -m 0644 "$source_dir/systemd/yunshao-agent.service" /usr/local/lib/systemd/system/yunshao-agent.service
}

if [ ! -x /usr/local/bin/yunshao-agent ] || ! /usr/local/bin/yunshao-agent activate --help 2>&1 | grep -q -- '--ticket-stdin'; then
  printf '__YUNSHAO_STAGE:installing_agent__\n'
  if ! command -v apt-get >/dev/null 2>&1; then
    printf '__YUNSHAO_ERROR:package_manager_unsupported__\n'
    exit 36
  fi
  export DEBIAN_FRONTEND=noninteractive
  if ! apt-get update -qq >/dev/null 2>&1 || ! apt-get install -y -qq build-essential cmake pkg-config libssl-dev libsqlite3-dev libcurl4-openssl-dev libmosquitto-dev nlohmann-json3-dev >/dev/null 2>&1; then
    printf '__YUNSHAO_ERROR:build_dependencies_unavailable__\n'
    exit 37
  fi
  if ! cmake -S "$source_dir" -B "$build_dir" -DCMAKE_BUILD_TYPE=Release -DYUNSHAO_AGENT_ONLY=ON -DBUILD_TESTING=OFF >/dev/null 2>&1 ||
     ! cmake --build "$build_dir" --target yunshao-agent -j2 >/dev/null 2>&1 ||
     ! cmake --install "$build_dir" --prefix /usr/local --component agent >/dev/null 2>&1; then
    printf '__YUNSHAO_ERROR:agent_build_failed__\n'
    exit 38
  fi
fi

ensure_service_user
systemctl daemon-reload >/dev/null 2>&1 || {
  printf '__YUNSHAO_ERROR:systemd_unavailable__\n'
  exit 39
}

printf '__YUNSHAO_STAGE:configuring_tls__\n'
printf '__YUNSHAO_CA_INPUT__\n'
IFS= read -r api_ca_b64 || { printf '__YUNSHAO_ERROR:api_ca_missing__\n'; exit 40; }
IFS= read -r mqtt_ca_b64 || { printf '__YUNSHAO_ERROR:mqtt_ca_missing__\n'; exit 41; }
if ! printf '%s' "$api_ca_b64" | base64 -d > "$work_dir/api-ca.pem" 2>/dev/null ||
   ! printf '%s' "$mqtt_ca_b64" | base64 -d > "$work_dir/mqtt-ca.pem" 2>/dev/null ||
   ! grep -q 'BEGIN CERTIFICATE' "$work_dir/api-ca.pem" ||
   ! grep -q 'BEGIN CERTIFICATE' "$work_dir/mqtt-ca.pem"; then
  printf '__YUNSHAO_ERROR:ca_invalid__\n'
  exit 42
fi
unset api_ca_b64 mqtt_ca_b64
install -m 0644 "$work_dir/api-ca.pem" "$work_dir/api-ca.pem.install"
install -m 0644 "$work_dir/mqtt-ca.pem" "$work_dir/mqtt-ca.pem.install"
mv -f "$work_dir/api-ca.pem.install" /etc/yunshao/api-ca.pem
mv -f "$work_dir/mqtt-ca.pem.install" /etc/yunshao/mqtt-ca.pem

printf '__YUNSHAO_STAGE:activating__\n'
printf '__YUNSHAO_TICKET_INPUT__\n'
IFS= read -r activation_ticket || { printf '__YUNSHAO_ERROR:activation_ticket_missing__\n'; exit 43; }
if ! printf '%s\n' "$activation_ticket" | /usr/local/bin/yunshao-agent activate \
  --server "$server_origin" \
  --device-id "$device_id" \
  --api-ca /etc/yunshao/api-ca.pem \
  --mqtt-ca /etc/yunshao/mqtt-ca.pem \
  --ticket-stdin; then
  unset activation_ticket
  printf '__YUNSHAO_ERROR:activation_failed__\n'
  exit 44
fi
unset activation_ticket
printf '__YUNSHAO_BOOTSTRAP_DONE__\n'
