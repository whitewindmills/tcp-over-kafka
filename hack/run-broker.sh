#!/usr/bin/env bash

# Start a local single-node Redpanda broker using the settings from the env file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

runtime="${BROKER_RUNTIME:-docker}"
image="$(broker_systemd_image)"
data_dir="$(broker_data_dir_for_mode systemd)"
rpc_port="${BROKER_RPC_PORT:-33145}"

require_command "$runtime"

# Resolve the advertised broker address from the configured Kafka endpoint.
broker_host=
broker_port=
split_host_port "${BROKER_ADDR}" broker_host broker_port
mkdir -p "$data_dir"

exec "$runtime" run --rm \
	--name tcp-over-kafka-broker \
	--network host \
	-v "${data_dir}:/var/lib/redpanda/data" \
	"${image}" \
	redpanda start \
	--mode dev-container \
	--check=false \
	--node-id 0 \
	--kafka-addr "internal://0.0.0.0:${broker_port},external://0.0.0.0:${broker_port}" \
	--advertise-kafka-addr "internal://127.0.0.1:${broker_port},external://${broker_host}:${broker_port}" \
	--rpc-addr "0.0.0.0:${rpc_port}" \
	--advertise-rpc-addr "${broker_host}:${rpc_port}"
