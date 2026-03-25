#!/usr/bin/env bash

# Deploy the broker, client, and server using systemd, docker, or kubernetes.
# Each component can pick its own mode via BROKER_DEPLOY_MODE, CLIENT_DEPLOY_MODE,
# and SERVER_DEPLOY_MODE, while DEPLOY_MODE provides the default.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

component="${1:-}"
[ -n "$component" ] || die "usage: deploy-remote.sh <broker|client|server|all>"

load_local_env

remote_bin_dir="${REMOTE_BIN_DIR:-/usr/local/bin}"
remote_libexec_dir="${REMOTE_LIBEXEC_DIR:-/usr/local/libexec/tcp-over-kafka}"
remote_env_dir="${REMOTE_ENV_DIR:-/etc/tcp-over-kafka}"
remote_env_file="${REMOTE_ENV_FILE:-${remote_env_dir}/tcp-over-kafka.env}"
remote_systemd_dir="${REMOTE_SYSTEMD_DIR:-/etc/systemd/system}"
remote_binary_path="${remote_bin_dir}/tcp-over-kafka"
remote_hello_script_path="${remote_bin_dir}/hello_https_server.py"
binary_path="${TCP_OVER_KAFKA_BINARY:-${PROJECT_ROOT}/bin/tcp-over-kafka}"
tunnel_runtime_image="${TUNNEL_RUNTIME_IMAGE:-gcr.io/distroless/static-debian12}"
hello_https_container_image="${HELLO_HTTPS_CONTAINER_IMAGE:-python:3.12-slim}"

ssh_args=(-o BatchMode=yes)
if [ -n "${SSH_AUTH:-}" ]; then
	ssh_args+=(-i "${SSH_AUTH}")
fi

# remote_spec returns user@host for an SSH-managed component.
remote_spec() {
	local target=$1
	printf '%s@%s' "$(remote_user_for "$target")" "$(remote_host_for "$target")"
}

# remote_exec runs one shell command on a remote host.
remote_exec() {
	local remote=$1
	local command=$2
	ssh "${ssh_args[@]}" "$remote" "$command"
}

# copy_remote_file stages a local file to a remote destination with a mode.
copy_remote_file() {
	local remote=$1
	local src=$2
	local dst=$3
	local mode=$4
	local tmp
	tmp="/tmp/$(basename "$dst").$$"

	scp "${ssh_args[@]}" "$src" "${remote}:${tmp}"
	remote_exec "$remote" "install -D -m ${mode} '${tmp}' '${dst}' && rm -f '${tmp}'"
}

# prepare_remote_dirs creates the directories used by systemd and docker deploys.
prepare_remote_dirs() {
	local remote=$1
	remote_exec "$remote" "install -d '${remote_bin_dir}' '${remote_libexec_dir}' '${remote_env_dir}' '${remote_systemd_dir}'"
}

# install_env_and_lib copies the shared env file and helper library to a host.
install_env_and_lib() {
	local remote=$1
	copy_remote_file "$remote" "${PROJECT_ROOT}/hack/.env.local" "${remote_env_file}" 0644
	copy_remote_file "$remote" "${SCRIPT_DIR}/lib.sh" "${remote_libexec_dir}/lib.sh" 0644
}

# install_tunnel_binary copies the locally built tunnel binary to a host.
install_tunnel_binary() {
	local remote=$1
	[ -x "$binary_path" ] || die "binary not found: $binary_path"
	copy_remote_file "$remote" "$binary_path" "${remote_binary_path}" 0755
}

# reload_systemd refreshes units and ensures the requested services are running.
reload_systemd() {
	local remote=$1
	local units=$2
	remote_exec "$remote" "systemctl daemon-reload && systemctl enable --now ${units}"
}

# build_client_args renders the client CLI args from the env file.
build_client_args() {
	local -n out=$1
	local route_flags=()
	append_json_flags route_flags --route "${CLIENT_ROUTES_JSON}"
	out=(
		client
		--topic "${KAFKA_TOPIC}"
		--broker "${BROKER_ADDR}"
		--group "${CLIENT_GROUP}"
		--listen "${CLIENT_LISTEN_ADDR}"
		--platform-id "${CLIENT_PLATFORM_ID}"
		--device-id "${CLIENT_DEVICE_ID}"
		--max-frame "${CLIENT_MAX_FRAME_SIZE:-32768}"
		"${route_flags[@]}"
	)
}

# build_server_args renders the server CLI args from the env file.
build_server_args() {
	local -n out=$1
	local service_flags=()
	append_json_flags service_flags --service "${SERVER_SERVICES_JSON}"
	out=(
		server
		--topic "${KAFKA_TOPIC}"
		--broker "${BROKER_ADDR}"
		--group "${SERVER_GROUP}"
		--platform-id "${SERVER_PLATFORM_ID}"
		--max-frame "${SERVER_MAX_FRAME_SIZE:-32768}"
		"${service_flags[@]}"
	)
}

# deploy_systemd_broker installs and enables the broker unit.
deploy_systemd_broker() {
	local remote
	remote="$(remote_spec broker)"
	prepare_remote_dirs "$remote"
	install_env_and_lib "$remote"
	copy_remote_file "$remote" "${SCRIPT_DIR}/run-broker.sh" "${remote_libexec_dir}/run-broker.sh" 0755
	copy_remote_file "$remote" "${SCRIPT_DIR}/stop-broker.sh" "${remote_libexec_dir}/stop-broker.sh" 0755
	copy_remote_file "$remote" "${PROJECT_ROOT}/deploy/systemd/tcp-over-kafka-broker.service" "${remote_systemd_dir}/tcp-over-kafka-broker.service" 0644
	reload_systemd "$remote" "tcp-over-kafka-broker"
}

# deploy_systemd_client installs and enables the client unit.
deploy_systemd_client() {
	local remote
	remote="$(remote_spec client)"
	prepare_remote_dirs "$remote"
	install_env_and_lib "$remote"
	install_tunnel_binary "$remote"
	copy_remote_file "$remote" "${SCRIPT_DIR}/run-client.sh" "${remote_libexec_dir}/run-client.sh" 0755
	copy_remote_file "$remote" "${PROJECT_ROOT}/deploy/systemd/tcp-over-kafka-client.service" "${remote_systemd_dir}/tcp-over-kafka-client.service" 0644
	reload_systemd "$remote" "tcp-over-kafka-client"
}

# deploy_systemd_server installs and enables the server and HTTPS helper units.
deploy_systemd_server() {
	local remote
	remote="$(remote_spec server)"
	prepare_remote_dirs "$remote"
	install_env_and_lib "$remote"
	install_tunnel_binary "$remote"
	copy_remote_file "$remote" "${SCRIPT_DIR}/run-server.sh" "${remote_libexec_dir}/run-server.sh" 0755
	copy_remote_file "$remote" "${SCRIPT_DIR}/run-hello-https.sh" "${remote_libexec_dir}/run-hello-https.sh" 0755
	copy_remote_file "$remote" "${SCRIPT_DIR}/hello_https_server.py" "${remote_hello_script_path}" 0755
	copy_remote_file "$remote" "${PROJECT_ROOT}/deploy/systemd/tcp-over-kafka-server.service" "${remote_systemd_dir}/tcp-over-kafka-server.service" 0644
	copy_remote_file "$remote" "${PROJECT_ROOT}/deploy/systemd/hello-world-https.service" "${remote_systemd_dir}/hello-world-https.service" 0644
	reload_systemd "$remote" "hello-world-https tcp-over-kafka-server"
}

# deploy_docker_broker runs the broker as a managed docker container.
deploy_docker_broker() {
	local remote broker_host broker_port
	remote="$(remote_spec broker)"
	require_command ssh
	require_command scp
	prepare_remote_dirs "$remote"
	broker_host=
	broker_port=
	split_host_port "${BROKER_ADDR}" broker_host broker_port

	local broker_cmd=(
		docker run -d --name tcp-over-kafka-broker --restart unless-stopped --network host
		-v "${BROKER_DATA_DIR}:/var/lib/redpanda/data"
		"${BROKER_IMAGE}"
		redpanda start
		--mode dev-container
		--check=false
		--node-id 0
		--kafka-addr "internal://0.0.0.0:${broker_port},external://0.0.0.0:${broker_port}"
		--advertise-kafka-addr "internal://127.0.0.1:${broker_port},external://${broker_host}:${broker_port}"
		--rpc-addr "0.0.0.0:${BROKER_RPC_PORT:-33145}"
		--advertise-rpc-addr "${broker_host}:${BROKER_RPC_PORT:-33145}"
	)

	remote_exec "$remote" "docker rm -f tcp-over-kafka-broker >/dev/null 2>&1 || true; install -d '${BROKER_DATA_DIR}'; $(shell_join "${broker_cmd[@]}")"
}

# deploy_docker_client runs the client in a docker container with host networking.
deploy_docker_client() {
	local remote
	local client_args=()
	remote="$(remote_spec client)"
	require_command ssh
	require_command scp
	prepare_remote_dirs "$remote"
	install_tunnel_binary "$remote"
	build_client_args client_args

	local docker_cmd=(
		docker run -d --name tcp-over-kafka-client --restart unless-stopped --network host
		-v "${remote_binary_path}:${remote_binary_path}:ro"
		"${tunnel_runtime_image}"
		"${remote_binary_path}"
		"${client_args[@]}"
	)

	remote_exec "$remote" "docker rm -f tcp-over-kafka-client >/dev/null 2>&1 || true; $(shell_join "${docker_cmd[@]}")"
}

# deploy_docker_server runs the HTTPS helper and server relay in docker containers.
deploy_docker_server() {
	local remote
	local server_args=()
	remote="$(remote_spec server)"
	require_command ssh
	require_command scp
	prepare_remote_dirs "$remote"
	install_tunnel_binary "$remote"
	copy_remote_file "$remote" "${SCRIPT_DIR}/hello_https_server.py" "${remote_hello_script_path}" 0755
	build_server_args server_args

	local hello_cmd=(
		docker run -d --name hello-world-https --restart unless-stopped --network host
		-v "${remote_hello_script_path}:/opt/tcp-over-kafka/hello_https_server.py:ro"
		-v "${HELLO_HTTPS_CERT}:${HELLO_HTTPS_CERT}:ro"
		-v "${HELLO_HTTPS_KEY}:${HELLO_HTTPS_KEY}:ro"
		"${hello_https_container_image}"
		python /opt/tcp-over-kafka/hello_https_server.py
		--bind "${HELLO_HTTPS_BIND:-0.0.0.0}"
		--port "${HELLO_HTTPS_PORT:-443}"
		--cert "${HELLO_HTTPS_CERT}"
		--key "${HELLO_HTTPS_KEY}"
	)

	local server_cmd=(
		docker run -d --name tcp-over-kafka-server --restart unless-stopped --network host
		-v "${remote_binary_path}:${remote_binary_path}:ro"
		"${tunnel_runtime_image}"
		"${remote_binary_path}"
		"${server_args[@]}"
	)

	remote_exec "$remote" "docker rm -f hello-world-https tcp-over-kafka-server >/dev/null 2>&1 || true; $(shell_join "${hello_cmd[@]}"); $(shell_join "${server_cmd[@]}")"
}

# deploy_kubernetes_component applies manifests for a component to the active cluster.
deploy_kubernetes_component() {
	local target=$1
	require_command kubectl
	case "$target" in
	client|server)
		local remote
		remote="$(remote_spec "$target")"
		require_command ssh
		require_command scp
		prepare_remote_dirs "$remote"
		install_tunnel_binary "$remote"
		;;
	esac
	"${SCRIPT_DIR}/render-kubernetes.sh" "$target" | kubectl apply -f -
}

# deploy_component dispatches to the requested deployment mode.
deploy_component() {
	local target=$1
	local mode
	mode="$(deploy_mode_for "$target")"
	case "$mode" in
	systemd)
		"deploy_systemd_${target}"
		;;
	docker)
		"deploy_docker_${target}"
		;;
	kubernetes)
		deploy_kubernetes_component "$target"
		;;
	*)
		die "unsupported deploy mode for ${target}: ${mode}"
		;;
	esac
}

case "$component" in
broker)
	deploy_component broker
	;;
client)
	deploy_component client
	;;
server)
	deploy_component server
	;;
all)
	deploy_component broker
	deploy_component client
	deploy_component server
	;;
*)
	die "unknown component: $component"
	;;
esac
