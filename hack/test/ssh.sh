#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

assert_node_ready node-a
assert_node_ready node-b

log "ssh check node-a -> node-b"
out_a_to_b="$(tunneled_ssh_capture node-a node-b 'hostname')"
[ -n "${out_a_to_b}" ] || die "empty hostname from node-b through node-a"

log "ssh check node-b -> node-a"
out_b_to_a="$(tunneled_ssh_capture node-b node-a 'hostname')"
[ -n "${out_b_to_a}" ] || die "empty hostname from node-a through node-b"

log "ssh tests passed"
