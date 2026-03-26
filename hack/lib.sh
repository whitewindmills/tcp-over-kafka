#!/usr/bin/env bash

# Shared helper functions for local validation and remote deployment scripts.

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_LOCAL_ENV_FILE="${PROJECT_ROOT}/hack/.env.local"
DEFAULT_REMOTE_ENV_FILE="/etc/tcp-over-kafka/tcp-over-kafka.env"

# log writes a namespaced message to stderr.
log() {
	printf '[tcp-over-kafka] %s\n' "$*" >&2
}

# die exits the current script with a formatted error.
die() {
	log "error: $*"
	exit 1
}

# require_command ensures a dependency is present before continuing.
require_command() {
	command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

# load_env_file exports the key/value pairs from a dotenv-style file.
load_env_file() {
	local env_file=$1
	[ -f "$env_file" ] || die "environment file not found: $env_file"
	set -a
	# shellcheck disable=SC1090
	. "$env_file"
	set +a
}

# load_local_env reads the tracked environment file from the repo.
load_local_env() {
	load_env_file "${TCP_OVER_KAFKA_LOCAL_ENV_FILE:-$DEFAULT_LOCAL_ENV_FILE}"
}

# load_runtime_env reads the deployed environment file on a remote host.
load_runtime_env() {
	load_env_file "${TCP_OVER_KAFKA_ENV_FILE:-$DEFAULT_REMOTE_ENV_FILE}"
}

# node_env_prefix_for returns the env var prefix for one logical node.
node_env_prefix_for() {
	case "$1" in
	node-a) printf '%s' "NODE_A" ;;
	node-b) printf '%s' "NODE_B" ;;
	*) die "unknown node: $1" ;;
	esac
}

# node_value resolves one env var for a logical node.
node_value() {
	local node=$1
	local suffix=$2
	local key
	key="$(node_env_prefix_for "$node")_${suffix}"
	printf '%s' "${!key:-}"
}

# node_config_path_for returns the deployed config path for a node.
node_config_path_for() {
	printf '%s' "${REMOTE_NODE_CONFIG_PATH:-/etc/tcp-over-kafka/node.json}"
}

# node_service_name_for returns the systemd/docker resource name for a node.
node_service_name_for() {
	case "$1" in
	node-a) printf '%s' "tcp-over-kafka-node-a" ;;
	node-b) printf '%s' "tcp-over-kafka-node-b" ;;
	*) die "unknown node: $1" ;;
	esac
}

# write_runtime_env_file renders the runtime variables needed by deployed services.
# SSH-only settings stay local and are intentionally excluded from the output file.
write_runtime_env_file() {
	local output_file=$1
	local key
	local -a runtime_keys=(
		BROKER_ADDR
		BROKER_RUNTIME
		BROKER_IMAGE
		BROKER_DATA_DIR
		BROKER_RPC_PORT
		TCP_OVER_KAFKA_NODE_CONFIG
		HELLO_HTTPS_BIND
		HELLO_HTTPS_PORT
		HELLO_HTTPS_CERT
		HELLO_HTTPS_KEY
	)

	: >"$output_file"
	printf '# Runtime settings rendered from %s\n' "${TCP_OVER_KAFKA_LOCAL_ENV_FILE:-$DEFAULT_LOCAL_ENV_FILE}" >>"$output_file"
	for key in "${runtime_keys[@]}"; do
		if [ -n "${!key+x}" ]; then
			printf '%s=%q\n' "$key" "${!key}" >>"$output_file"
		fi
	done
}

# write_node_config_file renders one node JSON config from the local env file.
write_node_config_file() {
	local node=$1
	local output_file=$2
	local platform_id listen_addr max_frame routes_json services_json

	require_command jq

	platform_id="$(node_value "$node" PLATFORM_ID)"
	listen_addr="$(node_value "$node" LISTEN_ADDR)"
	max_frame="$(node_value "$node" MAX_FRAME_SIZE)"
	routes_json="$(node_value "$node" ROUTES_JSON)"
	services_json="$(node_value "$node" SERVICES_JSON)"

	[ -n "$platform_id" ] || die "missing ${node} platform ID"
	[ -n "$listen_addr" ] || die "missing ${node} listen address"
	[ -n "$max_frame" ] || die "missing ${node} max frame size"
	[ -n "$routes_json" ] || die "missing ${node} routes json"
	[ -n "$services_json" ] || die "missing ${node} services json"

	jq -n \
		--arg broker "${BROKER_ADDR}" \
		--arg topic "${KAFKA_TOPIC}" \
		--arg platformID "${platform_id}" \
		--arg listen "${listen_addr}" \
		--argjson maxFrameSize "${max_frame}" \
		--argjson routes "${routes_json}" \
		--argjson services "${services_json}" \
		'{
			broker: $broker,
			topic: $topic,
			platformID: $platformID,
			listen: $listen,
			maxFrameSize: $maxFrameSize,
			routes: $routes,
			services: $services
		}' >"${output_file}"
}

# split_host_port breaks a host:port string into separate variables.
split_host_port() {
	local addr=$1
	local -n host_ref=$2
	local -n port_ref=$3

	if [[ "$addr" == \[*\]:* ]]; then
		host_ref="${addr%%]*}"
		host_ref="${host_ref#[}"
		port_ref="${addr##*:}"
		return
	fi

	host_ref="${addr%:*}"
	port_ref="${addr##*:}"
	[ -n "$host_ref" ] || die "invalid host:port value: $addr"
	[ -n "$port_ref" ] || die "invalid host:port value: $addr"
}

# remote_host_for returns the SSH host configured for a named component.
remote_host_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_SSH_HOST:-}" ;;
	node-a) printf '%s' "${NODE_A_SSH_HOST:-}" ;;
	node-b) printf '%s' "${NODE_B_SSH_HOST:-}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# remote_user_for returns the SSH user configured for a named component.
remote_user_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_SSH_USER:-${REMOTE_SSH_USER:-root}}" ;;
	node-a) printf '%s' "${NODE_A_SSH_USER:-${REMOTE_SSH_USER:-root}}" ;;
	node-b) printf '%s' "${NODE_B_SSH_USER:-${REMOTE_SSH_USER:-root}}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# deploy_mode_for returns the requested deploy mode for a component.
deploy_mode_for() {
	local key
	key="$(printf '%s' "$1" | tr '[:lower:]-' '[:upper:]_')_DEPLOY_MODE"
	printf '%s' "${!key:-${DEPLOY_MODE:-systemd}}"
}

# k8s_node_name_for returns the preferred Kubernetes node name for a component.
k8s_node_name_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_K8S_NODE_NAME:-${BROKER_SSH_HOST:-}}" ;;
	node-a) printf '%s' "${NODE_A_K8S_NODE_NAME:-${NODE_A_SSH_HOST:-}}" ;;
	node-b) printf '%s' "${NODE_B_K8S_NODE_NAME:-${NODE_B_SSH_HOST:-}}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# shell_join renders an argv-style list as shell-safe words.
shell_join() {
	printf '%q ' "$@"
}
