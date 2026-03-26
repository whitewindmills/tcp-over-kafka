#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

assert_node_ready node-a
assert_node_ready node-b

log "https check node-a -> node-b"
body_a_to_b="$(https_probe node-a node-b)"
[ "${body_a_to_b}" = "hello world" ] || die "unexpected https body from node-b: ${body_a_to_b}"

log "https check node-b -> node-a"
body_b_to_a="$(https_probe node-b node-a)"
[ "${body_b_to_a}" = "hello world" ] || die "unexpected https body from node-a: ${body_b_to_a}"

log "https tests passed"
