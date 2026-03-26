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

proxy_command_for() {
	local node=$1
	local socks_addr
	socks_addr="$(node_value "$node" SOCKS_ADDR)"
	[ -n "$socks_addr" ] || die "missing SOCKS address for ${node}"
	shell_join "${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$node")" /usr/local/bin/tcp-over-kafka proxy --socks "$socks_addr" %h %p
}

direct_remote_exec() {
	local node=$1
	shift
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$node")" "$@"
}

assert_node_ready() {
	local node=$1
	log "checking services on ${node}"
	direct_remote_exec "$node" systemctl is-active --quiet tcp-over-kafka-node
	direct_remote_exec "$node" systemctl is-active --quiet hello-world-https
}

tunneled_ssh_exec() {
	local source_node=$1
	local target_node=$2
	local remote_command=$3
	local proxy_command target_spec remote_shell

	proxy_command="$(proxy_command_for "$source_node")"
	target_spec="${SSH_TEST_USER}@$(remote_host_for "$target_node")"
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
	target_spec="${SSH_TEST_USER}@$(remote_host_for "$target_node"):${remote_path}"
	"${scp_cmd[@]}" "${ssh_args[@]}" "${scp_common_opts[@]}" -o "ProxyCommand=${proxy_command}" "$local_path" "$target_spec"
}

https_probe() {
	local source_node=$1
	local target_node=$2
	local proxy_command url

	proxy_command="$(proxy_command_for "$source_node")"
	url="https://$(remote_host_for "$target_node")/"
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$(local_remote_spec "$source_node")" \
		curl --silent --show-error --fail -k --proxy socks5h://"$(node_value "$source_node" SOCKS_ADDR)" "$url"
}

sha256_file() {
	sha256sum "$1" | awk '{print $1}'
}
