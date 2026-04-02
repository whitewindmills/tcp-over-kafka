#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

maybe_run_in_kubernetes_runner "$(basename "$0")"

bash "${SCRIPT_DIR}/ssh.sh"
bash "${SCRIPT_DIR}/https.sh"
bash "${SCRIPT_DIR}/file-transfer.sh"
