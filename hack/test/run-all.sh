#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

bash "${SCRIPT_DIR}/ssh.sh"
bash "${SCRIPT_DIR}/https.sh"
bash "${SCRIPT_DIR}/file-transfer.sh"
