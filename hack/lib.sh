#!/usr/bin/env bash

# Shared helper functions for local validation and remote deployment scripts.

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_LOCAL_ENV_FILE="${PROJECT_ROOT}/hack/.env.local"
DEFAULT_REMOTE_ENV_FILE="/etc/tcp-over-kafka/tcp-over-kafka.env"

# default_k8s_namespace is the namespace used when no explicit override is set.
default_k8s_namespace() {
	printf '%s' "default"
}

# default_k8s_route_host returns the stable logical route hostname for one node.
default_k8s_route_host() {
	case "$1" in
	node-a) printf '%s' "node-a.tcp-over-kafka.internal" ;;
	node-b) printf '%s' "node-b.tcp-over-kafka.internal" ;;
	*) die "unknown node: $1" ;;
	esac
}

# default_kubeconfig_path returns the standard local kubeconfig path.
default_kubeconfig_path() {
	printf '%s' "${HOME}/.kube/config"
}

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
	local key value
	local -A existing_keys=()
	[ -f "$env_file" ] || die "environment file not found: $env_file"

	# Preserve explicit caller-provided environment values so command-line
	# overrides remain authoritative over the checked-in defaults.
	while IFS='=' read -r -d '' key value; do
		if [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
			existing_keys["$key"]=1
		fi
	done < <(env -0)

	while IFS='=' read -r -d '' key value; do
		if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
			continue
		fi
		if [ -n "${existing_keys[$key]+x}" ]; then
			continue
		fi
		printf -v "$key" '%s' "$value"
		export "$key"
	done < <(
		set -a
		# shellcheck disable=SC1090
		. "$env_file"
		env -0
	)
}

# load_local_env reads the tracked environment file from the repo.
load_local_env() {
	prime_kubernetes_env_overrides
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

# node_listen_port returns the TCP port exposed by a logical node listener.
node_listen_port() {
	local listen_addr port
	listen_addr="$(node_value "$1" LISTEN_ADDR)"
	[ -n "$listen_addr" ] || die "missing listen address for $1"
	port="${listen_addr##*:}"
	[ -n "$port" ] || die "invalid listen address for $1: ${listen_addr}"
	printf '%s' "$port"
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
		BROKER_SYSTEMD_IMAGE
		BROKER_DATA_DIR
		BROKER_KRAFT_CLUSTER_ID
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
	local nid listen_addr max_frame routes_json services_json

	require_command jq

	nid="$(node_value "$node" NID)"
	listen_addr="$(node_value "$node" LISTEN_ADDR)"
	max_frame="$(node_value "$node" MAX_FRAME_SIZE)"
	routes_json="$(node_value "$node" ROUTES_JSON)"
	services_json="$(node_value "$node" SERVICES_JSON)"

	[ -n "$nid" ] || die "missing ${node} NID"
	[ -n "$listen_addr" ] || die "missing ${node} listen address"
	[ -n "$max_frame" ] || die "missing ${node} max frame size"
	[ -n "$routes_json" ] || die "missing ${node} routes json"
	[ -n "$services_json" ] || die "missing ${node} services json"

	jq -n \
		--arg broker "${BROKER_ADDR}" \
		--arg topic "${KAFKA_TOPIC}" \
		--arg nid "${nid}" \
		--arg listen "${listen_addr}" \
		--argjson maxFrameSize "${max_frame}" \
		--argjson routes "${routes_json}" \
		--argjson services "${services_json}" \
		'{
			broker: $broker,
			topic: $topic,
			nid: $nid,
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

# k8s_namespace returns the active Kubernetes namespace.
k8s_namespace() {
	k8s_namespace_for broker
}

# k8s_namespace_for returns the Kubernetes namespace for one component.
k8s_namespace_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_KUBERNETES_NAMESPACE:-${KUBERNETES_NAMESPACE:-$(default_k8s_namespace)}}" ;;
	node-a) printf '%s' "${NODE_A_KUBERNETES_NAMESPACE:-${KUBERNETES_NAMESPACE:-$(default_k8s_namespace)}}" ;;
	node-b) printf '%s' "${NODE_B_KUBERNETES_NAMESPACE:-${KUBERNETES_NAMESPACE:-$(default_k8s_namespace)}}" ;;
	*) die "unknown kubernetes component: $1" ;;
	esac
}

# dual_kubernetes_node_mode returns success when both logical nodes deploy to Kubernetes.
dual_kubernetes_node_mode() {
	[ "$(deploy_mode_for node-a)" = "kubernetes" ] && [ "$(deploy_mode_for node-b)" = "kubernetes" ]
}

# k8s_kubeconfig_env_value_for returns the effective KUBECONFIG value for a component.
k8s_kubeconfig_env_value_for() {
	case "$1" in
	broker)
		if [ -n "${BROKER_KUBECONFIG:-}" ]; then
			printf '%s' "${BROKER_KUBECONFIG}"
			return
		fi
		;;
	node-a)
		if [ -n "${NODE_A_KUBECONFIG:-}" ]; then
			printf '%s' "${NODE_A_KUBECONFIG}"
			return
		fi
		if dual_kubernetes_node_mode; then
			die "dual-kubernetes node mode requires NODE_A_KUBECONFIG; refusing to fall back to a shared default kubeconfig"
		fi
		;;
	node-b)
		if [ -n "${NODE_B_KUBECONFIG:-}" ]; then
			printf '%s' "${NODE_B_KUBECONFIG}"
			return
		fi
		if dual_kubernetes_node_mode; then
			die "dual-kubernetes node mode requires NODE_B_KUBECONFIG; refusing to fall back to a shared default kubeconfig"
		fi
		;;
	*)
		die "unknown kubernetes component: $1"
		;;
	esac
	if [ -n "${KUBECONFIG:-}" ]; then
		printf '%s' "${KUBECONFIG}"
		return
	fi
	printf '%s' "$(default_kubeconfig_path)"
}

# k8s_kubectl runs kubectl with the kubeconfig and namespace for one component.
k8s_kubectl() {
	local component=$1
	shift
	local namespace kubeconfig_value
	namespace="$(k8s_namespace_for "$component")"
	kubeconfig_value="$(k8s_kubeconfig_env_value_for "$component")"
	if [ -n "${kubeconfig_value}" ]; then
		KUBECONFIG="${kubeconfig_value}" kubectl --namespace "${namespace}" "$@"
		return
	fi
	kubectl --namespace "${namespace}" "$@"
}

# k8s_config_view runs `kubectl config view` with the component kubeconfig.
k8s_config_view() {
	local component=$1
	shift
	local kubeconfig_value
	kubeconfig_value="$(k8s_kubeconfig_env_value_for "$component")"
	if [ -n "${kubeconfig_value}" ]; then
		KUBECONFIG="${kubeconfig_value}" kubectl config view "$@"
		return
	fi
	kubectl config view "$@"
}

# require_kubernetes_kubeconfig_files validates that the effective kubeconfig files exist locally.
require_kubernetes_kubeconfig_files() {
	local component=$1
	local kubeconfig_value file
	local -a kubeconfig_files=()
	kubeconfig_value="$(k8s_kubeconfig_env_value_for "$component")"
	IFS=':' read -r -a kubeconfig_files <<<"${kubeconfig_value}"
	for file in "${kubeconfig_files[@]}"; do
		[ -n "${file}" ] || continue
		[ -f "${file}" ] || die "kubeconfig file not found for ${component}: ${file}"
	done
}

# k8s_api_server_for returns the configured Kubernetes API server URL for one component.
k8s_api_server_for() {
	k8s_config_view "$1" --minify --raw -o jsonpath='{.clusters[0].cluster.server}'
}

# require_dual_kubernetes_node_separation prevents both logical nodes from
# silently targeting the same cluster unless they are pinned to distinct
# Kubernetes node names.
require_dual_kubernetes_node_separation() {
	local node_a_server node_b_server node_a_name node_b_name

	dual_kubernetes_node_mode || return 0

	node_a_server="$(k8s_api_server_for node-a)"
	node_b_server="$(k8s_api_server_for node-b)"
	[ -n "${node_a_server}" ] || die "failed to read kubernetes API server from kubeconfig for node-a"
	[ -n "${node_b_server}" ] || die "failed to read kubernetes API server from kubeconfig for node-b"

	if [ "${node_a_server}" != "${node_b_server}" ]; then
		return 0
	fi

	node_a_name="$(k8s_node_name_for node-a)"
	node_b_name="$(k8s_node_name_for node-b)"
	if [ -z "${node_a_name}" ] || [ -z "${node_b_name}" ]; then
		die "node-a and node-b resolve to the same kubernetes API server (${node_a_server}); configure distinct NODE_A_KUBECONFIG/NODE_B_KUBECONFIG or set distinct NODE_A_K8S_NODE_NAME/NODE_B_K8S_NODE_NAME"
	fi
	[ "${node_a_name}" != "${node_b_name}" ] || die "node-a and node-b resolve to the same kubernetes API server (${node_a_server}) and the same Kubernetes node name (${node_a_name})"
}

# require_kubernetes_api_host_resolution validates that the kubeconfig API server hostname resolves locally.
require_kubernetes_api_host_resolution() {
	local component=$1
	local server_url server_host
	require_command getent
	server_url="$(k8s_api_server_for "$component")"
	[ -n "${server_url}" ] || die "failed to read kubernetes API server from kubeconfig for ${component}"
	server_host="${server_url#*://}"
	server_host="${server_host%%/*}"
	if [[ "${server_host}" == \[*\]* ]]; then
		server_host="${server_host%%]*}"
		server_host="${server_host#[}"
	else
		server_host="${server_host%%:*}"
	fi
	getent hosts "${server_host}" >/dev/null 2>&1 || die "kubernetes API hostname does not resolve locally for ${component}: ${server_host}"
}

# require_kubernetes_fixture_material validates the local HTTPS and SSH inputs needed for node fixtures.
require_kubernetes_fixture_material() {
	[ -f "${HELLO_HTTPS_CERT}" ] || die "https cert not found: ${HELLO_HTTPS_CERT}"
	[ -f "${HELLO_HTTPS_KEY}" ] || die "https key not found: ${HELLO_HTTPS_KEY}"
	if [ -n "${SSH_AUTH:-}" ] && [ -f "${SSH_AUTH}" ]; then
		if [ ! -f "${SSH_AUTH}.pub" ]; then
			require_command ssh-keygen
		fi
		return
	fi
	[ -n "${SSH_PASSWORD:-}" ] || die "missing SSH auth for kubernetes fixtures; configure SSH_AUTH or SSH_PASSWORD"
}

# require_kubernetes_component_prereqs validates the local prerequisites for one Kubernetes-managed component.
require_kubernetes_component_prereqs() {
	local component=$1
	require_command kubectl
	require_kubernetes_kubeconfig_files "$component"
	require_kubernetes_api_host_resolution "$component"
	case "$component" in
	node-a | node-b)
		require_dual_kubernetes_node_separation
		require_kubernetes_fixture_material
		;;
	esac
}

# k8s_node_service_name returns the ClusterIP service name for one logical node.
k8s_node_service_name() {
	printf '%s' "tcp-over-kafka-$1"
}

# k8s_node_service_dns returns the fully qualified service DNS name for one node.
k8s_node_service_dns() {
	printf '%s.%s.svc.cluster.local' "$(k8s_node_service_name "$1")" "$(k8s_namespace_for "$1")"
}

# k8s_node_config_map_name returns the ConfigMap name carrying one node config.
k8s_node_config_map_name() {
	printf '%s' "$(k8s_node_service_name "$1")-config"
}

# k8s_node_https_secret_name returns the Secret name carrying one node cert/key pair.
k8s_node_https_secret_name() {
	printf '%s' "$(k8s_node_service_name "$1")-https"
}

# k8s_ssh_auth_secret_name returns the Secret name for shared SSH test auth.
k8s_ssh_auth_secret_name() {
	printf '%s' "tcp-over-kafka-ssh-auth"
}

# k8s_e2e_runner_name returns the pod name for the in-cluster validation runner.
k8s_e2e_runner_name() {
	local node=${1:-}
	if [ -n "${node}" ]; then
		printf 'tcp-over-kafka-e2e-runner-%s' "${node}"
		return
	fi
	printf '%s' "tcp-over-kafka-e2e-runner"
}

# deploy_mode_for returns the requested deploy mode for a component.
deploy_mode_for() {
	local key
	key="$(printf '%s' "$1" | tr '[:lower:]-' '[:upper:]_')_DEPLOY_MODE"
	printf '%s' "${!key:-${DEPLOY_MODE:-systemd}}"
}

# tunnel_runtime_image returns the cluster-pullable tunnel runtime image reference.
tunnel_runtime_image() {
	printf '%s' "${KUBERNETES_TUNNEL_RUNTIME_IMAGE:-image.cestc.cn/ccos-test/tcp-over-kafka:latest}"
}

# tunnel_fixture_image returns the helper image used by Kubernetes fixtures and E2E runs.
tunnel_fixture_image() {
	printf '%s' "${TUNNEL_FIXTURE_IMAGE:-image.cestc.cn/ccos-test/tcp-over-kafka-fixture:latest}"
}

# target_host_for returns the host that tests should use for a logical node.
target_host_for() {
	local route_host
	route_host="$(node_value "$1" ROUTE_HOST)"
	if [ -n "${route_host}" ]; then
		printf '%s' "${route_host}"
		return
	fi
	case "$(deploy_mode_for "$1")" in
	kubernetes) default_k8s_route_host "$1" ;;
	*) remote_host_for "$1" ;;
	esac
}

# node_local_access_host_for returns the host that local checks should use for a node runtime.
node_local_access_host_for() {
	case "$(deploy_mode_for "$1")" in
	kubernetes) k8s_node_service_dns "$1" ;;
	*) remote_host_for "$1" ;;
	esac
}

# node_socks_addr_for returns the SOCKS listener address for a logical node.
node_socks_addr_for() {
	case "$(deploy_mode_for "$1")" in
	kubernetes) printf '%s:%s' "$(node_local_access_host_for "$1")" "$(node_listen_port "$1")" ;;
	*) node_value "$1" SOCKS_ADDR ;;
	esac
}

# broker_systemd_image returns the image used by the systemd-managed Redpanda broker.
broker_systemd_image() {
	printf '%s' "${BROKER_SYSTEMD_IMAGE:-docker.redpanda.com/redpandadata/redpanda:v25.1.6}"
}

# broker_container_image returns the image used by Docker and Kubernetes broker deploys.
broker_container_image() {
	printf '%s' "${BROKER_IMAGE:-apache/kafka:3.9.2}"
}

# broker_data_dir_for_mode returns the host-side data directory for the active broker mode.
broker_data_dir_for_mode() {
	case "$1" in
	systemd) printf '%s' "${BROKER_DATA_DIR:-/var/lib/redpanda}" ;;
	docker | kubernetes) printf '%s' "${BROKER_DATA_DIR:-/var/lib/kafka}" ;;
	*) die "unknown broker mode: $1" ;;
	esac
}

# broker_kraft_cluster_id returns the default cluster ID for single-node Kafka KRaft mode.
broker_kraft_cluster_id() {
	printf '%s' "${BROKER_KRAFT_CLUSTER_ID:-abcdefghijklmnopqrstuv}"
}

# k8s_node_name_for returns the preferred Kubernetes node name for a component.
k8s_node_name_for() {
	case "$1" in
	broker) printf '%s' "${BROKER_K8S_NODE_NAME:-${BROKER_SSH_HOST:-}}" ;;
	node-a) printf '%s' "${NODE_A_K8S_NODE_NAME:-}" ;;
	node-b) printf '%s' "${NODE_B_K8S_NODE_NAME:-}" ;;
	*) die "unknown component: $1" ;;
	esac
}

# prime_kubernetes_env_overrides seeds route-host overrides before the env file
# is sourced so the default ROUTES_JSON expressions resolve to stable logical names.
prime_kubernetes_env_overrides() {
	local node_a_mode node_b_mode

	node_a_mode="${NODE_A_DEPLOY_MODE:-${DEPLOY_MODE:-systemd}}"
	node_b_mode="${NODE_B_DEPLOY_MODE:-${DEPLOY_MODE:-systemd}}"

	if [ "${node_a_mode}" = "kubernetes" ] && [ -z "${NODE_A_ROUTE_HOST+x}" ]; then
		export NODE_A_ROUTE_HOST
		NODE_A_ROUTE_HOST="$(default_k8s_route_host node-a)"
	fi
	if [ "${node_b_mode}" = "kubernetes" ] && [ -z "${NODE_B_ROUTE_HOST+x}" ]; then
		export NODE_B_ROUTE_HOST
		NODE_B_ROUTE_HOST="$(default_k8s_route_host node-b)"
	fi
}

# shell_join renders an argv-style list as shell-safe words.
shell_join() {
	printf '%q ' "$@"
}
