#!/usr/bin/env bash

# Execute one shell validation entrypoint from inside the Kubernetes cluster network.
# The dual-cluster path runs the script once from node-a's cluster and once from node-b's cluster.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

script_name="${1:-}"
[ -n "${script_name}" ] || die "usage: run-kubernetes-e2e.sh <ssh.sh|https.sh|file-transfer.sh|run-all.sh|concurrency.sh>"

load_local_env
require_command kubectl

peer_node_for() {
	case "$1" in
	node-a) printf '%s' "node-b" ;;
	node-b) printf '%s' "node-a" ;;
	*) die "unknown node: $1" ;;
	esac
}

runner_key_path() {
	if [ -n "${SSH_AUTH:-}" ] && [ -f "${SSH_AUTH}" ]; then
		printf '%s' "/etc/tcp-over-kafka-ssh/id_key"
	fi
}

run_direction_from_cluster() {
	local local_node=$1
	local remote_node runner_name
	local -a exec_env=()

	remote_node="$(peer_node_for "${local_node}")"
	runner_name="$(k8s_e2e_runner_name "${local_node}")"

	log "starting kubernetes E2E runner for ${local_node} -> ${remote_node}"
	require_kubernetes_component_prereqs "${local_node}"

	k8s_kubectl "${local_node}" delete pod "${runner_name}" --ignore-not-found >/dev/null 2>&1 || true
	bash "${SCRIPT_DIR}/render-kubernetes-e2e.sh" "${local_node}" | k8s_kubectl "${local_node}" apply -f -
	k8s_kubectl "${local_node}" wait --for=condition=Ready "pod/${runner_name}" --timeout="${KUBERNETES_RUNNER_TIMEOUT:-180s}"
	k8s_kubectl "${local_node}" exec "${runner_name}" -- rm -rf /opt/tcp-over-kafka-tests
	k8s_kubectl "${local_node}" exec "${runner_name}" -- mkdir -p /opt/tcp-over-kafka-tests
	k8s_kubectl "${local_node}" cp "${PROJECT_ROOT}/hack" "${runner_name}:/opt/tcp-over-kafka-tests"

	exec_env=(
		TCP_OVER_KAFKA_LOCAL_ENV_FILE=/opt/tcp-over-kafka-tests/hack/.env.local
		TCP_OVER_KAFKA_IN_CLUSTER_E2E=1
		BROKER_DEPLOY_MODE="${BROKER_DEPLOY_MODE:-$(deploy_mode_for broker)}"
		NODE_A_DEPLOY_MODE="${NODE_A_DEPLOY_MODE:-$(deploy_mode_for node-a)}"
		NODE_B_DEPLOY_MODE="${NODE_B_DEPLOY_MODE:-$(deploy_mode_for node-b)}"
		E2E_LOCAL_NODE="${local_node}"
		E2E_REMOTE_NODE="${remote_node}"
	)
	if [ -n "${SSH_PASSWORD:-}" ]; then
		exec_env+=(SSH_PASSWORD="${SSH_PASSWORD}")
	fi
	if [ -n "$(runner_key_path)" ]; then
		exec_env+=(SSH_AUTH="$(runner_key_path)")
	fi

	k8s_kubectl "${local_node}" exec "${runner_name}" -- \
		env "${exec_env[@]}" \
		bash -lc "cd /opt/tcp-over-kafka-tests && bash ./hack/test/${script_name}"
}

run_direction_from_cluster node-a
run_direction_from_cluster node-b
