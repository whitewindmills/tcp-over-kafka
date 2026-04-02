#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

maybe_run_in_kubernetes_runner "$(basename "$0")"

run_ssh_check() {
	local source_node=$1
	local target_node=$2
	local output

	log "ssh check ${source_node} -> ${target_node}"
	output="$(tunneled_ssh_capture "${source_node}" "${target_node}" 'hostname')"
	[ -n "${output}" ] || die "empty hostname from ${target_node} through ${source_node}"
}

if directional_e2e_mode; then
	assert_node_ready "${E2E_LOCAL_NODE}"
	run_ssh_check "${E2E_LOCAL_NODE}" "${E2E_REMOTE_NODE}"
else
	assert_node_ready node-a
	assert_node_ready node-b
	run_ssh_check node-a node-b
	run_ssh_check node-b node-a
fi

log "ssh tests passed"
