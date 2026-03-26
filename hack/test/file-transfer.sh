#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

assert_node_ready node-a
assert_node_ready node-b

work_dir="$(mktemp -d)"
cleanup() {
	rm -rf "${work_dir}"
}
trap cleanup EXIT

direct_remote_exec node-a install -d "${SCP_REMOTE_DIR}"
direct_remote_exec node-b install -d "${SCP_REMOTE_DIR}"

copy_and_verify() {
	local source_node=$1
	local target_node=$2
	local size_mb=$3
	local local_path remote_path local_sum remote_sum remote_cmd

	local_path="${work_dir}/${source_node}-to-${target_node}-${size_mb}MiB.bin"
	remote_path="${SCP_REMOTE_DIR}/$(basename "${local_path}")"

	log "file transfer ${source_node} -> ${target_node} (${size_mb} MiB)"
	dd if=/dev/urandom of="${local_path}" bs=1M count="${size_mb}" status=none
	tunneled_scp_to "${source_node}" "${target_node}" "${local_path}" "${remote_path}"

	local_sum="$(sha256_file "${local_path}")"
	remote_cmd="sha256sum $(printf '%q' "${remote_path}") | awk '{print \$1}'"
	remote_sum="$(tunneled_ssh_capture "${source_node}" "${target_node}" "${remote_cmd}")"

	[ "${local_sum}" = "${remote_sum}" ] || die "checksum mismatch for ${source_node} -> ${target_node} (${size_mb} MiB)"
	direct_remote_exec "${target_node}" rm -f "${remote_path}"
}

IFS=',' read -r -a sizes <<<"${E2E_FILE_SIZES_MB}"
for size_mb in "${sizes[@]}"; do
	copy_and_verify node-a node-b "${size_mb}"
	copy_and_verify node-b node-a "${size_mb}"
done

log "file transfer tests passed"
