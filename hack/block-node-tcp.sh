#!/usr/bin/env bash

# Add or remove remote firewall rules that block all TCP traffic between
# node-a and node-b. The rules are applied directly on each host with iptables.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

action="${1:-apply}"
rule_tag="tcp-over-kafka-node-peer-block"

load_local_env

ssh_cmd=(ssh)
ssh_args=()
ssh_common_opts=(
	-o StrictHostKeyChecking=no
	-o UserKnownHostsFile=/dev/null
	-o LogLevel=ERROR
)
if [ -n "${SSH_AUTH:-}" ] && [ -f "${SSH_AUTH}" ]; then
	ssh_args=(-o BatchMode=yes -i "${SSH_AUTH}")
elif [ -n "${SSH_PASSWORD:-}" ]; then
	if [ -n "${SSH_AUTH:-}" ]; then
		log "ssh identity file not found at ${SSH_AUTH}; falling back to password authentication"
	fi
	require_command sshpass
	export SSHPASS="${SSH_PASSWORD}"
	ssh_cmd=(sshpass -e ssh)
	ssh_args=(
		-o BatchMode=no
		-o NumberOfPasswordPrompts=1
		-o PreferredAuthentications=password
		-o PubkeyAuthentication=no
	)
else
	ssh_args=(-o BatchMode=yes)
fi

remote_spec() {
	local target=$1
	printf '%s@%s' "$(remote_user_for "$target")" "$(remote_host_for "$target")"
}

apply_remote_rules() {
	local node=$1
	local peer_host=$2
	local remote
	remote="$(remote_spec "$node")"

	log "${action} TCP peer-block rules on ${node} for ${peer_host}"
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$remote" bash -s -- "$action" "$peer_host" "$rule_tag" <<'EOF'
set -euo pipefail

action=$1
peer_host=$2
rule_tag=$3

command -v iptables >/dev/null 2>&1 || {
	printf 'iptables is not installed on %s\n' "$(hostname)" >&2
	exit 1
}

iptables_cmd=(iptables -w)
input_rule=(-s "$peer_host" -p tcp -m comment --comment "$rule_tag" -j DROP)
output_rule=(-d "$peer_host" -p tcp -m comment --comment "$rule_tag" -j DROP)

case "$action" in
apply)
	"${iptables_cmd[@]}" -C INPUT "${input_rule[@]}" >/dev/null 2>&1 || "${iptables_cmd[@]}" -I INPUT 1 "${input_rule[@]}"
	"${iptables_cmd[@]}" -C OUTPUT "${output_rule[@]}" >/dev/null 2>&1 || "${iptables_cmd[@]}" -I OUTPUT 1 "${output_rule[@]}"
	;;
remove)
	while "${iptables_cmd[@]}" -C INPUT "${input_rule[@]}" >/dev/null 2>&1; do
		"${iptables_cmd[@]}" -D INPUT "${input_rule[@]}"
	done
	while "${iptables_cmd[@]}" -C OUTPUT "${output_rule[@]}" >/dev/null 2>&1; do
		"${iptables_cmd[@]}" -D OUTPUT "${output_rule[@]}"
	done
	;;
status)
	printf 'INPUT rules on %s:\n' "$(hostname)"
	"${iptables_cmd[@]}" -S INPUT | grep -- "$rule_tag" || true
	printf 'OUTPUT rules on %s:\n' "$(hostname)"
	"${iptables_cmd[@]}" -S OUTPUT | grep -- "$rule_tag" || true
	exit 0
	;;
*)
	printf 'unsupported action: %s\n' "$action" >&2
	exit 1
	;;
esac

printf '%s complete on %s for peer %s\n' "$action" "$(hostname)" "$peer_host"
EOF
}

case "$action" in
apply | remove | status) ;;
*)
	die "usage: block-node-tcp.sh [apply|remove|status]"
	;;
esac

node_a_host="$(remote_host_for node-a)"
node_b_host="$(remote_host_for node-b)"
[ -n "$node_a_host" ] || die "missing NODE_A_SSH_HOST"
[ -n "$node_b_host" ] || die "missing NODE_B_SSH_HOST"

apply_remote_rules node-a "$node_b_host"
apply_remote_rules node-b "$node_a_host"

log "done"
