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

# append_json_flags converts a JSON object into repeated CLI flags.
append_json_flags() {
	local -n out=$1
	local flag_name=$2
	local json_map=$3
	local entry

	require_command jq
	mapfile -t out < <(printf '%s' "$json_map" | jq -r 'to_entries[] | "\(.key)=\(.value)"')
	local rendered=()
	for entry in "${out[@]}"; do
		rendered+=("$flag_name" "$entry")
	done
	out=("${rendered[@]}")
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
	client) printf '%s' "${CLIENT_SSH_HOST:-}" ;;
	server) printf '%s' "${SERVER_SSH_HOST:-}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# remote_user_for returns the SSH user configured for a named component.
remote_user_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_SSH_USER:-${REMOTE_SSH_USER:-root}}" ;;
	client) printf '%s' "${CLIENT_SSH_USER:-${REMOTE_SSH_USER:-root}}" ;;
	server) printf '%s' "${SERVER_SSH_USER:-${REMOTE_SSH_USER:-root}}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# deploy_mode_for returns the requested deploy mode for a component.
deploy_mode_for() {
	local key
	key="$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')_DEPLOY_MODE"
	printf '%s' "${!key:-${DEPLOY_MODE:-systemd}}"
}

# k8s_node_name_for returns the preferred Kubernetes node name for a component.
k8s_node_name_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_K8S_NODE_NAME:-${BROKER_SSH_HOST:-}}" ;;
	client) printf '%s' "${CLIENT_K8S_NODE_NAME:-${CLIENT_SSH_HOST:-}}" ;;
	server) printf '%s' "${SERVER_K8S_NODE_NAME:-${SERVER_SSH_HOST:-}}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# shell_join renders an argv-style list as shell-safe words.
shell_join() {
	printf '%q ' "$@"
}
