#!/bin/sh
set -eu

options=/run/secrets/mosquitto_ctrl_options
run_ctrl() {
	mosquitto_ctrl -o "$options" -i yunshao-broker-bootstrap dynsec "$@"
}

field_equals() {
	output="$1"
	field="$2"
	expected="$3"
	printf '%s\n' "$output" | awk -F: -v field="$field" -v expected="$expected" '
		$1 == field {
			value = $2
			gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
			if (value == expected) found = 1
		}
		END { exit !found }
	'
}

get_role() {
	run_ctrl getRole "$1" 2>&1 || true
}

ensure_role() {
	role="$1"
	role_info="$(get_role "$role")"
	if ! field_equals "$role_info" Rolename "$role"; then
		run_ctrl createRole "$role" >/dev/null 2>&1 || true
		role_info="$(get_role "$role")"
	fi
	if ! field_equals "$role_info" Rolename "$role"; then
		echo "Mosquitto Dynamic Security role initialization failed: $role" >&2
		return 1
	fi
}

ensure_acl() {
	role="$1"
	acl_type="$2"
	topic="$3"
	role_info="$(get_role "$role")"
	if ! field_equals "$role_info" Rolename "$role"; then
		echo "Mosquitto Dynamic Security ACL role is missing: $role" >&2
		return 1
	fi
	if ! printf '%s\n' "$role_info" | grep -F "$acl_type" | grep -F "$topic" | grep -F 'allow' >/dev/null; then
		run_ctrl addRoleACL "$role" "$acl_type" "$topic" allow 100 >/dev/null 2>&1 || true
		role_info="$(get_role "$role")"
	fi
	if ! printf '%s\n' "$role_info" | grep -F "$acl_type" | grep -F "$topic" | grep -F 'allow' >/dev/null; then
		echo "Mosquitto Dynamic Security ACL initialization failed: $role $acl_type $topic" >&2
		return 1
	fi
}

ensure_service_client() {
	username="$1"
	client_id="$2"
	password_file="$3"
	role="$4"
	client_info="$(run_ctrl getClient "$username" 2>&1 || true)"
	if ! field_equals "$client_info" Username "$username"; then
		password="$(cat "$password_file")"
		if [ -z "$password" ]; then
			echo "Mosquitto Dynamic Security client password is empty: $username" >&2
			return 1
		fi
		printf '%s\n%s\n' "$password" "$password" | run_ctrl createClient "$username" -i "$client_id" >/dev/null 2>&1 || true
		client_info="$(run_ctrl getClient "$username" 2>&1 || true)"
	fi
	if ! field_equals "$client_info" Username "$username"; then
		echo "Mosquitto Dynamic Security client initialization failed: $username" >&2
		return 1
	fi
	if ! field_equals "$client_info" Clientid "$client_id"; then
		run_ctrl setClientId "$username" "$client_id" >/dev/null 2>&1 || true
		client_info="$(run_ctrl getClient "$username" 2>&1 || true)"
	fi
	if ! field_equals "$client_info" Clientid "$client_id"; then
		echo "Mosquitto Dynamic Security client ID initialization failed: $username" >&2
		return 1
	fi
	if ! printf '%s\n' "$client_info" | grep -F "$role (priority:" >/dev/null; then
		run_ctrl addClientRole "$username" "$role" 100 >/dev/null 2>&1 || true
		client_info="$(run_ctrl getClient "$username" 2>&1 || true)"
	fi
	if ! printf '%s\n' "$client_info" | grep -F "$role (priority:" >/dev/null; then
		echo "Mosquitto Dynamic Security client role initialization failed: $username $role" >&2
		return 1
	fi
}

ensure_role yunshao-device-service
ensure_acl yunshao-device-service subscribePattern 'devices/+/state'
ensure_acl yunshao-device-service publishClientReceive 'devices/+/state'
ensure_acl yunshao-device-service subscribePattern 'devices/+/command-acks'
ensure_acl yunshao-device-service publishClientReceive 'devices/+/command-acks'
ensure_acl yunshao-device-service publishClientSend 'devices/+/commands'
ensure_service_client yunshao-device-service yunshao-device-service /run/secrets/mqtt_device_service_password yunshao-device-service

ensure_role yunshao-event-service
ensure_acl yunshao-event-service subscribePattern 'devices/+/events'
ensure_acl yunshao-event-service publishClientReceive 'devices/+/events'
ensure_acl yunshao-event-service publishClientSend 'devices/+/event-acks'
ensure_service_client yunshao-event-service yunshao-event-service /run/secrets/mqtt_event_service_password yunshao-event-service

echo "Mosquitto Dynamic Security service identities are ready"
