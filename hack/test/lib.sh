#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/../lib.sh"

load_local_env

ssh_cmd=(ssh)
scp_cmd=(scp)
ssh_args=()
if [ -n "${SSH_AUTH:-}" ] && [ -f "${SSH_AUTH}" ]; then
	ssh_args=(-o BatchMode=yes -i "${SSH_AUTH}")
elif [ -n "${SSH_PASSWORD:-}" ]; then
	if [ -n "${SSH_AUTH:-}" ]; then
		log "ssh identity file not found at ${SSH_AUTH}; falling back to password authentication"
	fi
	require_command sshpass
	export SSHPASS="${SSH_PASSWORD}"
	ssh_cmd=(sshpass -e ssh)
	scp_cmd=(sshpass -e scp)
	ssh_args=(
		-o BatchMode=no
		-o NumberOfPasswordPrompts=1
		-o PreferredAuthentications=password
		-o PubkeyAuthentication=no
	)
else
	ssh_args=(-o BatchMode=yes)
fi

ssh_common_opts=(
	-T
	-o StrictHostKeyChecking=no
	-o UserKnownHostsFile=/dev/null
	-o LogLevel=ERROR
)

scp_common_opts=(
	-o StrictHostKeyChecking=no
	-o UserKnownHostsFile=/dev/null
	-o LogLevel=ERROR
)

local_remote_spec() {
	local node=$1
	printf '%s@%s' "$(remote_user_for "$node")" "$(remote_host_for "$node")"
}

kubernetes_test_mode() {
	[ "$(deploy_mode_for node-a)" = "kubernetes" ] && [ "$(deploy_mode_for node-b)" = "kubernetes" ]
}

directional_e2e_mode() {
	[ -n "${E2E_LOCAL_NODE:-}" ] && [ -n "${E2E_REMOTE_NODE:-}" ]
}

maybe_run_in_kubernetes_runner() {
	local script_name=$1
	if [ -n "${TCP_OVER_KAFKA_IN_CLUSTER_E2E:-}" ]; then
		return 0
	fi
	if kubernetes_test_mode; then
		bash "${SCRIPT_DIR}/../run-kubernetes-e2e.sh" "${script_name}"
		exit $?
	fi
	if [ "$(deploy_mode_for node-a)" = "kubernetes" ] || [ "$(deploy_mode_for node-b)" = "kubernetes" ]; then
		die "mixed Kubernetes and non-Kubernetes node validation is not supported"
	fi
	return 0
}

wait_for_tcp_endpoint() {
	local host=$1
	local port=$2
	local attempts=${3:-30}
	local i

	for i in $(seq 1 "${attempts}"); do
		if (exec 3<>"/dev/tcp/${host}/${port}") >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	die "tcp endpoint is not reachable: ${host}:${port}"
}

peer_node_for() {
	case "$1" in
	node-a) printf '%s' "node-b" ;;
	node-b) printf '%s' "node-a" ;;
	*) die "unknown node: $1" ;;
	esac
}

proxy_command_for() {
	local node=$1
	local socks_addr
	socks_addr="$(node_socks_addr_for "$node")"
	[ -n "$socks_addr" ] || die "missing SOCKS address for ${node}"
	if [ "$(deploy_mode_for "$node")" = "kubernetes" ]; then
		shell_join /usr/local/bin/tcp-over-kafka proxy --socks "$socks_addr" %h %p
		return
	fi
	if [ -n "${SSH_PASSWORD:-}" ] && { [ -z "${SSH_AUTH:-}" ] || [ ! -f "${SSH_AUTH:-}" ]; }; then
		shell_join env "SSHPASS=${SSH_PASSWORD}" "${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$node")" /usr/local/bin/tcp-over-kafka proxy --socks "$socks_addr" %h %p
		return
	fi
	shell_join "${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$node")" /usr/local/bin/tcp-over-kafka proxy --socks "$socks_addr" %h %p
}

direct_remote_exec() {
	local node=$1
	shift
	if [ "$(deploy_mode_for "$node")" = "kubernetes" ]; then
		die "direct host execution is not supported for kubernetes-deployed ${node}"
	fi
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$node")" "$@"
}

assert_docker_container_running() {
	local node=$1
	local container=$2
	local running

	running="$(direct_remote_exec "$node" bash -lc "docker inspect -f '{{.State.Running}}' ${container} 2>/dev/null || true")"
	[ "${running}" = "true" ] || die "container ${container} is not running on ${node}"
}

assert_node_ready() {
	local node=$1
	local mode

	mode="$(deploy_mode_for "$node")"
	log "checking services on ${node} (${mode})"
	case "$mode" in
	systemd)
		direct_remote_exec "$node" systemctl is-active --quiet tcp-over-kafka-node
		direct_remote_exec "$node" systemctl is-active --quiet hello-world-https
		;;
	docker)
		assert_docker_container_running "$node" tcp-over-kafka-node
		assert_docker_container_running "$node" hello-world-https
		;;
	kubernetes)
		wait_for_tcp_endpoint "$(node_local_access_host_for "$node")" "$(node_listen_port "$node")"
		wait_for_tcp_endpoint "$(node_local_access_host_for "$node")" 22
		wait_for_tcp_endpoint "$(node_local_access_host_for "$node")" "${HELLO_HTTPS_PORT:-443}"
		;;
	*)
		die "node readiness checks do not support deploy mode ${mode} for ${node}"
		;;
	esac
}

tunneled_ssh_exec() {
	local source_node=$1
	local target_node=$2
	local remote_command=$3
	local proxy_command target_spec remote_shell

	proxy_command="$(proxy_command_for "$source_node")"
	target_spec="${SSH_TEST_USER}@$(target_host_for "$target_node")"
	printf -v remote_shell "bash -lc %q" "$remote_command"
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" -o "ProxyCommand=${proxy_command}" "$target_spec" "$remote_shell"
}

tunneled_ssh_capture() {
	local source_node=$1
	local target_node=$2
	local remote_command=$3
	tunneled_ssh_exec "$source_node" "$target_node" "$remote_command"
}

tunneled_scp_to() {
	local source_node=$1
	local target_node=$2
	local local_path=$3
	local remote_path=$4
	local proxy_command target_spec

	proxy_command="$(proxy_command_for "$source_node")"
	target_spec="${SSH_TEST_USER}@$(target_host_for "$target_node"):${remote_path}"
	"${scp_cmd[@]}" "${ssh_args[@]}" "${scp_common_opts[@]}" -o "ProxyCommand=${proxy_command}" "$local_path" "$target_spec"
}

https_probe() {
	local source_node=$1
	local target_node=$2
	local url

	url="https://$(target_host_for "$target_node")/"
	if [ "$(deploy_mode_for "$source_node")" = "kubernetes" ]; then
		curl --silent --show-error --fail -k --proxy "socks5h://$(node_socks_addr_for "$source_node")" "$url"
		return
	fi
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$source_node")" \
		curl --silent --show-error --fail -k --proxy "socks5h://$(node_socks_addr_for "$source_node")" "$url"
}

sha256_file() {
	sha256sum "$1" | awk '{print $1}'
}
