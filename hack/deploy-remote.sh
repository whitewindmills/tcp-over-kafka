#!/usr/bin/env bash

# Deploy the broker and symmetric node runtimes using systemd, docker, or kubernetes.
# Each component can pick its own mode via BROKER_DEPLOY_MODE, NODE_A_DEPLOY_MODE,
# and NODE_B_DEPLOY_MODE, while DEPLOY_MODE provides the default.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

component="${1:-}"
[ -n "$component" ] || die "usage: deploy-remote.sh <broker|node-a|node-b|all>"

load_local_env

remote_bin_dir="${REMOTE_BIN_DIR:-/usr/local/bin}"
remote_libexec_dir="${REMOTE_LIBEXEC_DIR:-/usr/local/libexec/tcp-over-kafka}"
remote_env_dir="${REMOTE_ENV_DIR:-/etc/tcp-over-kafka}"
remote_env_file="${REMOTE_ENV_FILE:-${remote_env_dir}/tcp-over-kafka.env}"
remote_systemd_dir="${REMOTE_SYSTEMD_DIR:-/etc/systemd/system}"
remote_binary_path="${remote_bin_dir}/tcp-over-kafka"
remote_hello_script_path="${remote_bin_dir}/hello_https_server.py"
remote_node_config_path="$(node_config_path_for node-a)"
binary_path="${TCP_OVER_KAFKA_BINARY:-${PROJECT_ROOT}/bin/tcp-over-kafka}"
tunnel_runtime_image="${TUNNEL_RUNTIME_IMAGE:-gcr.io/distroless/static-debian12}"
hello_https_container_image="${HELLO_HTTPS_CONTAINER_IMAGE:-python:3.12-slim}"
kubernetes_runtime_image="$(tunnel_runtime_image)"
fixture_container_image="$(tunnel_fixture_image)"
k8s_images_published=0

ssh_cmd=(ssh)
scp_cmd=(scp)
ssh_args=()
if [ -n "${SSH_AUTH:-}" ] && [ -f "${SSH_AUTH}" ]; then
	ssh_args=(-o BatchMode=yes -i "${SSH_AUTH}")
elif [ -n "${SSH_PASSWORD:-}" ]; then
	if [ -n "${SSH_AUTH:-}" ]; then
		log "ssh identity file not found at ${SSH_AUTH}; falling back to password authentication"
	fi
	require_command sshpass
	export SSHPASS="${SSH_PASSWORD}"
	ssh_cmd=(sshpass -e ssh)
	scp_cmd=(sshpass -e scp)
	ssh_args=(
		-o BatchMode=no
		-o NumberOfPasswordPrompts=1
		-o PreferredAuthentications=password
		-o PubkeyAuthentication=no
	)
else
	ssh_args=(-o BatchMode=yes)
fi

ssh_common_opts=(
	-o StrictHostKeyChecking=no
	-o UserKnownHostsFile=/dev/null
	-o LogLevel=ERROR
)

scp_common_opts=(
	-o StrictHostKeyChecking=no
	-o UserKnownHostsFile=/dev/null
	-o LogLevel=ERROR
)

# remote_spec returns user@host for an SSH-managed component.
remote_spec() {
	local target=$1
	printf '%s@%s' "$(remote_user_for "$target")" "$(remote_host_for "$target")"
}

# remote_exec runs one shell command on a remote host.
remote_exec() {
	local remote=$1
	local command=$2
	"${ssh_cmd[@]}" "${ssh_args[@]}" "${ssh_common_opts[@]}" "$remote" "$command"
}

# copy_remote_file stages a local file to a remote destination with a mode.
copy_remote_file() {
	local remote=$1
	local src=$2
	local dst=$3
	local mode=$4
	local tmp
	tmp="/tmp/$(basename "$dst").$$"

	"${scp_cmd[@]}" "${ssh_args[@]}" "${scp_common_opts[@]}" "$src" "${remote}:${tmp}"
	remote_exec "$remote" "install -D -m ${mode} '${tmp}' '${dst}' && rm -f '${tmp}'"
}

# stage_remote_container_image pulls a container image locally, copies it to a
# remote host, and loads it there so the remote runtime does not need registry access.
stage_remote_container_image() {
	local remote=$1
	local image=$2
	local archive remote_archive

	require_command docker
	require_command gzip

	archive="$(mktemp "${TMPDIR:-/tmp}/tcp-over-kafka-image.XXXXXX.tar.gz")"
	remote_archive="/tmp/$(basename "${archive}")"

	docker pull "${image}"
	docker save "${image}" | gzip -1 >"${archive}"
	"${scp_cmd[@]}" "${ssh_args[@]}" "${scp_common_opts[@]}" "${archive}" "${remote}:${remote_archive}"
	remote_exec "$remote" "gzip -dc '${remote_archive}' | docker load >/dev/null && rm -f '${remote_archive}'"
	rm -f "${archive}"
}

# publish_kubernetes_images builds and pushes the runtime and fixture images once.
publish_kubernetes_images() {
	local build_version

	if [ "${k8s_images_published}" -eq 1 ]; then
		return 0
	fi

	require_command docker
	build_version="${KUBERNETES_IMAGE_BUILD_VERSION:-$(date -u +%Y%m%d%H%M%S)}"
	export KUBERNETES_IMAGE_BUILD_VERSION="${build_version}"

	docker build --build-arg VERSION="${build_version}" -t "${kubernetes_runtime_image}" "${PROJECT_ROOT}"
	docker push "${kubernetes_runtime_image}"
	docker build --build-arg VERSION="${build_version}" -f "${PROJECT_ROOT}/hack/Dockerfile.fixture" -t "${fixture_container_image}" "${PROJECT_ROOT}"
	docker push "${fixture_container_image}"

	k8s_images_published=1
}

# prepare_remote_dirs creates the directories used by systemd and docker deploys.
prepare_remote_dirs() {
	local remote=$1
	remote_exec "$remote" "install -d '${remote_bin_dir}' '${remote_libexec_dir}' '${remote_env_dir}' '${remote_systemd_dir}'"
}

# install_env_and_lib copies the shared env file and helper library to a host.
install_env_and_lib() {
	local remote=$1
	local rendered_env
	rendered_env="$(mktemp)"
	TCP_OVER_KAFKA_NODE_CONFIG="${remote_node_config_path}" write_runtime_env_file "${rendered_env}"
	copy_remote_file "$remote" "${rendered_env}" "${remote_env_file}" 0644
	rm -f "${rendered_env}"
	copy_remote_file "$remote" "${SCRIPT_DIR}/lib.sh" "${remote_libexec_dir}/lib.sh" 0644
}

# install_tunnel_binary copies the locally built tunnel binary to a host.
install_tunnel_binary() {
	local remote=$1
	[ -x "$binary_path" ] || die "binary not found: $binary_path"
	copy_remote_file "$remote" "$binary_path" "${remote_binary_path}" 0755
}

# install_node_config copies one rendered node JSON config to a host.
install_node_config() {
	local remote=$1
	local node=$2
	local rendered_config
	rendered_config="$(mktemp)"
	write_node_config_file "$node" "${rendered_config}"
	copy_remote_file "$remote" "${rendered_config}" "${remote_node_config_path}" 0644
	rm -f "${rendered_config}"
}

# reload_systemd refreshes units and ensures the requested services are running.
reload_systemd() {
	local remote=$1
	local units=$2
	remote_exec "$remote" "systemctl daemon-reload && systemctl enable --now ${units}"
}

# retire_legacy_node_units disables the old split-role units so only the
# symmetric node runtime consumes the shared Kafka topic after a node deploy.
retire_legacy_node_units() {
	local remote=$1
	remote_exec "$remote" "systemctl disable --now tcp-over-kafka-client tcp-over-kafka-server >/dev/null 2>&1 || true; systemctl reset-failed tcp-over-kafka-client tcp-over-kafka-server >/dev/null 2>&1 || true"
}

# retire_legacy_node_containers removes the old split-role docker workloads so
# the node runtime can claim the host-network ports and Kafka consumer role.
retire_legacy_node_containers() {
	local remote=$1
	remote_exec "$remote" "docker rm -f tcp-over-kafka-client tcp-over-kafka-server hello-world-https >/dev/null 2>&1 || true"
}

# retire_legacy_node_kubernetes deletes the old split-role kubernetes resources
# before the symmetric node deployments are applied.
retire_legacy_node_kubernetes() {
	local node=$1
	local namespace
	namespace="$(k8s_namespace_for "$node")"
	if ! k8s_kubectl "$node" get namespace "${namespace}" >/dev/null 2>&1; then
		return 0
	fi
	k8s_kubectl "$node" delete deployment tcp-over-kafka-client tcp-over-kafka-server --ignore-not-found >/dev/null
	k8s_kubectl "$node" delete configmap tcp-over-kafka-hello-script --ignore-not-found >/dev/null
}

# wait_for_kubernetes_deployment blocks until the named deployment reports ready.
wait_for_kubernetes_deployment() {
	local target=$1
	local deployment=$2
	k8s_kubectl "$target" rollout status "deployment/${deployment}" --timeout="${KUBERNETES_ROLLOUT_TIMEOUT:-180s}"
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
	remote_exec "$remote" "docker rm -f tcp-over-kafka-broker >/dev/null 2>&1 || true"
	reload_systemd "$remote" "tcp-over-kafka-broker"
}

# deploy_external_broker validates an existing broker without modifying it.
deploy_external_broker() {
	local remote broker_host broker_port
	remote="$(remote_spec broker)"
	broker_host=
	broker_port=
	split_host_port "${BROKER_ADDR}" broker_host broker_port

	if [ "$broker_host" != "$(remote_host_for broker)" ]; then
		die "external broker host mismatch: BROKER_ADDR=${BROKER_ADDR}, BROKER_SSH_HOST=$(remote_host_for broker)"
	fi

	remote_exec "$remote" "systemctl disable --now tcp-over-kafka-broker >/dev/null 2>&1 || true; systemctl reset-failed tcp-over-kafka-broker >/dev/null 2>&1 || true"
	remote_exec "$remote" "ss -ltn | awk '\$4 ~ /:${broker_port}\$/ && \$1 == \"LISTEN\" { found=1 } END { exit(found ? 0 : 1) }'" \
		|| die "external broker is not listening on ${BROKER_ADDR}"

	log "broker deploy mode is external; leaving existing broker on ${BROKER_ADDR} unchanged"
}

# deploy_systemd_node installs and enables the node and HTTPS helper units.
deploy_systemd_node() {
	local node=$1
	local remote
	remote="$(remote_spec "$node")"
	prepare_remote_dirs "$remote"
	install_env_and_lib "$remote"
	install_tunnel_binary "$remote"
	install_node_config "$remote" "$node"
	copy_remote_file "$remote" "${SCRIPT_DIR}/run-node.sh" "${remote_libexec_dir}/run-node.sh" 0755
	copy_remote_file "$remote" "${SCRIPT_DIR}/run-hello-https.sh" "${remote_libexec_dir}/run-hello-https.sh" 0755
	copy_remote_file "$remote" "${SCRIPT_DIR}/hello_https_server.py" "${remote_hello_script_path}" 0755
	copy_remote_file "$remote" "${PROJECT_ROOT}/deploy/systemd/tcp-over-kafka-node.service" "${remote_systemd_dir}/tcp-over-kafka-node.service" 0644
	copy_remote_file "$remote" "${PROJECT_ROOT}/deploy/systemd/hello-world-https.service" "${remote_systemd_dir}/hello-world-https.service" 0644
	retire_legacy_node_units "$remote"
	reload_systemd "$remote" "hello-world-https tcp-over-kafka-node"
}

# deploy_docker_broker runs the broker as a managed docker container.
deploy_docker_broker() {
	local remote broker_host broker_port broker_data_dir broker_image cluster_id
	remote="$(remote_spec broker)"
	prepare_remote_dirs "$remote"
	broker_host=
	broker_port=
	split_host_port "${BROKER_ADDR}" broker_host broker_port
	broker_data_dir="$(broker_data_dir_for_mode docker)"
	broker_image="$(broker_container_image)"
	cluster_id="$(broker_kraft_cluster_id)"
	local broker_cmd=(
		docker run -d --name tcp-over-kafka-broker --restart unless-stopped --network host
		--user 0:0
		-v "${broker_data_dir}:/var/lib/kafka/data:Z"
		-e CLUSTER_ID="${cluster_id}"
		-e KAFKA_NODE_ID=1
		-e KAFKA_PROCESS_ROLES=broker,controller
		-e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT
		-e KAFKA_ADVERTISED_LISTENERS="PLAINTEXT://${broker_host}:${broker_port}"
		-e KAFKA_LISTENERS="PLAINTEXT://:${broker_port},CONTROLLER://127.0.0.1:9093"
		-e KAFKA_INTER_BROKER_LISTENER_NAME=PLAINTEXT
		-e KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER
		-e KAFKA_CONTROLLER_QUORUM_VOTERS=1@127.0.0.1:9093
		-e KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1
		-e KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS=0
		-e KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1
		-e KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1
		-e KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR=1
		-e KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR=1
		-e KAFKA_LOG_DIRS=/var/lib/kafka/data
		"${broker_image}"
	)

	stage_remote_container_image "$remote" "$broker_image"

	remote_exec "$remote" "systemctl disable --now tcp-over-kafka-broker >/dev/null 2>&1 || true; systemctl reset-failed tcp-over-kafka-broker >/dev/null 2>&1 || true; docker rm -f tcp-over-kafka-broker >/dev/null 2>&1 || true; docker rm -f tcp-over-kafka-zookeeper >/dev/null 2>&1 || true; install -d '${broker_data_dir}'; rm -rf '${broker_data_dir}'/*; chown -R 0:0 '${broker_data_dir}'; $(shell_join "${broker_cmd[@]}")"
	remote_exec "$remote" "for i in \$(seq 1 30); do ss -ltn | awk '\$4 ~ /:${broker_port}\$/ && \$1 == \"LISTEN\" { found=1 } END { exit(found ? 0 : 1) }' && exit 0; sleep 1; done; docker logs --tail 80 tcp-over-kafka-broker >&2; exit 1"
}

# deploy_docker_node runs the HTTPS helper and node runtime in docker containers.
deploy_docker_node() {
	local node=$1
	local remote
	remote="$(remote_spec "$node")"
	prepare_remote_dirs "$remote"
	install_env_and_lib "$remote"
	install_tunnel_binary "$remote"
	install_node_config "$remote" "$node"
	copy_remote_file "$remote" "${SCRIPT_DIR}/hello_https_server.py" "${remote_hello_script_path}" 0755

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

	local node_cmd=(
		docker run -d --name tcp-over-kafka-node --restart unless-stopped --network host
		-v "${remote_binary_path}:${remote_binary_path}:ro"
		-v "${remote_node_config_path}:${remote_node_config_path}:ro"
		"${tunnel_runtime_image}"
		"${remote_binary_path}"
		node
		--config "${remote_node_config_path}"
	)

	retire_legacy_node_containers "$remote"
	stage_remote_container_image "$remote" "$hello_https_container_image"
	stage_remote_container_image "$remote" "$tunnel_runtime_image"
	remote_exec "$remote" "docker rm -f tcp-over-kafka-node >/dev/null 2>&1 || true; $(shell_join "${hello_cmd[@]}"); $(shell_join "${node_cmd[@]}")"
}

# deploy_kubernetes_component applies manifests for a component to the active cluster.
deploy_kubernetes_component() {
	local target=$1
	require_kubernetes_component_prereqs "$target"
	case "$target" in
	node-a|node-b)
		publish_kubernetes_images
		retire_legacy_node_kubernetes "$target"
		;;
	broker)
		;;
	*)
		die "unknown kubernetes target: ${target}"
		;;
	esac
	bash "${SCRIPT_DIR}/render-kubernetes.sh" "$target" | k8s_kubectl "$target" apply -f -
	case "$target" in
	node-a|node-b)
		wait_for_kubernetes_deployment "$target" "$(k8s_node_service_name "$target")"
		;;
	broker)
		wait_for_kubernetes_deployment broker tcp-over-kafka-broker
		;;
	esac
}

# deploy_component dispatches to the requested deployment mode.
deploy_component() {
	local target=$1
	local mode
	mode="$(deploy_mode_for "$target")"
	case "$mode" in
	systemd)
		case "$target" in
		broker) deploy_systemd_broker ;;
		node-a | node-b) deploy_systemd_node "$target" ;;
		*) die "unknown component: $target" ;;
		esac
		;;
	docker)
		case "$target" in
		broker) deploy_docker_broker ;;
		node-a | node-b) deploy_docker_node "$target" ;;
		*) die "unknown component: $target" ;;
		esac
		;;
	kubernetes)
		deploy_kubernetes_component "$target"
		;;
	external)
		case "$target" in
		broker) deploy_external_broker ;;
		*) die "unsupported external deploy mode for ${target}" ;;
		esac
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
node-a)
	deploy_component node-a
	;;
node-b)
	deploy_component node-b
	;;
all)
	deploy_component broker
	deploy_component node-a
	deploy_component node-b
	;;
*)
	die "unknown component: $component"
	;;
esac
