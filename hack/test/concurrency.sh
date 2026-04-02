#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

maybe_run_in_kubernetes_runner "$(basename "$0")"

sweeps="${E2E_CONCURRENCY_SWEEPS:-1,5,8,10}"
work_dir="$(mktemp -d)"
overall_status=0

cleanup() {
	rm -rf "${work_dir}"
}
trap cleanup EXIT

run_batch() {
	local label=$1
	local count=$2
	local script_name=$3
	local i log_path
	local -a pids=()
	local passed=0

	for i in $(seq 1 "${count}"); do
		log_path="${work_dir}/${label}-${count}-${i}.log"
		bash "${SCRIPT_DIR}/${script_name}" >"${log_path}" 2>&1 &
		pids+=("$!")
	done

	for i in "${!pids[@]}"; do
		if wait "${pids[$i]}"; then
			passed=$((passed + 1))
			continue
		fi
		log "${label} failure $(($i + 1))/${count}: $(tr '\n' ' ' <"${work_dir}/${label}-${count}-$(($i + 1)).log")"
	done

	printf '%s' "${passed}"
}

IFS=',' read -r -a counts <<<"${sweeps}"
for count in "${counts[@]}"; do
	log "concurrency sweep ${count} SSH + ${count} HTTPS"
	ssh_pass="$(run_batch ssh "${count}" ssh.sh)"
	https_pass="$(run_batch https "${count}" https.sh)"
	log "sweep ${count}: ssh ${ssh_pass}/${count}, https ${https_pass}/${count}"
	if [ "${ssh_pass}" -ne "${count}" ] || [ "${https_pass}" -ne "${count}" ]; then
		overall_status=1
	fi
done

exit "${overall_status}"
