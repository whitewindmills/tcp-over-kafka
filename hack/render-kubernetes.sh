#!/usr/bin/env bash

# Render kubernetes manifests for the broker and symmetric node components.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

component="${1:-}"
[ -n "$component" ] || die "usage: render-kubernetes.sh <broker|node-a|node-b|all>"

load_local_env
fixture_image="$(tunnel_fixture_image)"
runtime_image="$(tunnel_runtime_image)"

case "$component" in
broker)
	require_kubernetes_component_prereqs broker
	;;
node-a | node-b)
	require_kubernetes_component_prereqs "$component"
	;;
all)
	if [ "${BROKER_DEPLOY_MODE:-$(deploy_mode_for broker)}" = "kubernetes" ]; then
		require_kubernetes_component_prereqs broker
	fi
	require_kubernetes_component_prereqs node-a
	require_kubernetes_component_prereqs node-b
	;;
*)
	die "unknown component: $component"
	;;
esac

# yaml_escape escapes a string so it can be embedded in a double-quoted YAML scalar.
yaml_escape() {
	local value=$1
	value=${value//\\/\\\\}
	value=${value//\"/\\\"}
	printf '%s' "$value"
}

# print_yaml_list writes a YAML list at a fixed indentation level.
print_yaml_list() {
	local indent=$1
	shift
	local item
	for item in "$@"; do
		printf "%${indent}s- \"%s\"\n" "" "$(yaml_escape "$item")"
	done
}

# print_optional_node_name emits a nodeName field when one is configured.
print_optional_node_name() {
	local indent=$1
	local value=$2
	if [ -n "$value" ]; then
		printf "%${indent}snodeName: \"%s\"\n" "" "$(yaml_escape "$value")"
	fi
}

# print_build_annotations emits a build annotation when one is present.
print_build_annotations() {
	local indent=$1
	if [ -z "${KUBERNETES_IMAGE_BUILD_VERSION:-}" ]; then
		return
	fi
	printf "%${indent}sannotations:\n" ""
	printf "%$((indent + 2))stcp-over-kafka/build-version: \"%s\"\n" "" "$(yaml_escape "${KUBERNETES_IMAGE_BUILD_VERSION}")"
}

# print_yaml_block prints a multi-line scalar under an already-open parent map.
print_yaml_block() {
	local indent=$1
	local key=$2
	local content=$3
	printf "%${indent}s%s: |\n" "" "$key"
	printf '%s\n' "$content" | sed "s/^/$(printf "%${indent}s" "")  /"
}

# file_block prints the contents of a text file as a YAML block scalar.
file_block() {
	local indent=$1
	local key=$2
	local path=$3
	print_yaml_block "$indent" "$key" "$(cat "$path")"
}

# ssh_private_key_path resolves the locally configured private key if present.
ssh_private_key_path() {
	if [ -n "${SSH_AUTH:-}" ] && [ -f "${SSH_AUTH}" ]; then
		printf '%s' "${SSH_AUTH}"
	fi
}

# ssh_authorized_key_material returns the public key used by the SSH fixture.
ssh_authorized_key_material() {
	local private_key public_key
	private_key="$(ssh_private_key_path)"
	if [ -z "$private_key" ]; then
		return 0
	fi
	public_key="${private_key}.pub"
	if [ -f "$public_key" ]; then
		cat "$public_key"
		return 0
	fi
	require_command ssh-keygen
	ssh-keygen -y -f "$private_key"
}

render_namespace() {
	local namespace=$1
	cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
EOF
}

render_broker() {
	local namespace broker_host broker_port node_name broker_data_dir cluster_id
	namespace="$(k8s_namespace_for broker)"
	broker_host=
	broker_port=
	split_host_port "${BROKER_ADDR}" broker_host broker_port
	node_name="$(k8s_node_name_for broker)"
	broker_data_dir="$(broker_data_dir_for_mode kubernetes)"
	cluster_id="$(broker_kraft_cluster_id)"

	cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tcp-over-kafka-broker
  namespace: ${namespace}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tcp-over-kafka-broker
  template:
    metadata:
      labels:
        app: tcp-over-kafka-broker
EOF
	print_build_annotations 6
	cat <<EOF
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
EOF
	print_optional_node_name 6 "$node_name"
	cat <<EOF
      containers:
      - name: broker
        image: $(broker_container_image)
        securityContext:
          runAsUser: 0
          runAsGroup: 0
        env:
        - name: CLUSTER_ID
          value: "${cluster_id}"
        - name: KAFKA_NODE_ID
          value: "1"
        - name: KAFKA_PROCESS_ROLES
          value: "broker,controller"
        - name: KAFKA_LISTENER_SECURITY_PROTOCOL_MAP
          value: "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT"
        - name: KAFKA_ADVERTISED_LISTENERS
          value: "PLAINTEXT://${broker_host}:${broker_port}"
        - name: KAFKA_LISTENERS
          value: "PLAINTEXT://:${broker_port},CONTROLLER://127.0.0.1:9093"
        - name: KAFKA_INTER_BROKER_LISTENER_NAME
          value: "PLAINTEXT"
        - name: KAFKA_CONTROLLER_LISTENER_NAMES
          value: "CONTROLLER"
        - name: KAFKA_CONTROLLER_QUORUM_VOTERS
          value: "1@127.0.0.1:9093"
        - name: KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR
          value: "1"
        - name: KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS
          value: "0"
        - name: KAFKA_TRANSACTION_STATE_LOG_MIN_ISR
          value: "1"
        - name: KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR
          value: "1"
        - name: KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR
          value: "1"
        - name: KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR
          value: "1"
        - name: KAFKA_LOG_DIRS
          value: "/var/lib/kafka/data"
        volumeMounts:
        - name: broker-data
          mountPath: /var/lib/kafka/data
      volumes:
      - name: broker-data
        hostPath:
          path: ${broker_data_dir}
          type: DirectoryOrCreate
EOF
}

render_ssh_auth_secret() {
	local node=$1
	local namespace private_key authorized_key
	namespace="$(k8s_namespace_for "$node")"

	private_key="$(ssh_private_key_path)"
	authorized_key="$(ssh_authorized_key_material)"
	if [ -z "${SSH_PASSWORD:-}" ] && [ -z "$authorized_key" ]; then
		die "missing SSH auth for kubernetes fixtures; configure SSH_AUTH or SSH_PASSWORD"
	fi

	cat <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: $(k8s_ssh_auth_secret_name)
  namespace: ${namespace}
type: Opaque
stringData:
EOF
	if [ -n "$authorized_key" ]; then
		print_yaml_block 2 authorized_keys "$authorized_key"
	fi
	if [ -n "$private_key" ]; then
		file_block 2 id_key "$private_key"
	fi
	if [ -n "${SSH_PASSWORD:-}" ]; then
		print_yaml_block 2 password "${SSH_PASSWORD}"
	fi
}

render_node_config_map() {
	local node=$1
	local namespace rendered_config
	namespace="$(k8s_namespace_for "$node")"

	rendered_config="$(mktemp)"
	write_node_config_file "$node" "${rendered_config}"

	cat <<EOF
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: $(k8s_node_config_map_name "$node")
  namespace: ${namespace}
data:
EOF
	file_block 2 node.json "${rendered_config}"
	rm -f "${rendered_config}"
}

render_node_https_secret() {
	local node=$1
	local namespace
	namespace="$(k8s_namespace_for "$node")"
	[ -f "${HELLO_HTTPS_CERT}" ] || die "https cert not found: ${HELLO_HTTPS_CERT}"
	[ -f "${HELLO_HTTPS_KEY}" ] || die "https key not found: ${HELLO_HTTPS_KEY}"

	cat <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: $(k8s_node_https_secret_name "$node")
  namespace: ${namespace}
type: Opaque
stringData:
EOF
	file_block 2 server.pem "${HELLO_HTTPS_CERT}"
	file_block 2 server.key "${HELLO_HTTPS_KEY}"
}

render_node_service() {
	local node=$1
	local namespace service_name socks_port
	namespace="$(k8s_namespace_for "$node")"

	service_name="$(k8s_node_service_name "$node")"
	socks_port="$(node_listen_port "$node")"

	cat <<EOF
---
apiVersion: v1
kind: Service
metadata:
  name: ${service_name}
  namespace: ${namespace}
spec:
  selector:
    app: ${service_name}
  ports:
  - name: socks
    port: ${socks_port}
    targetPort: socks
  - name: ssh
    port: 22
    targetPort: ssh
  - name: https
    port: ${HELLO_HTTPS_PORT:-443}
    targetPort: https
EOF
}

render_node_deployment() {
	local node=$1
	local namespace deployment_name node_name socks_port
	namespace="$(k8s_namespace_for "$node")"

	deployment_name="$(k8s_node_service_name "$node")"
	node_name="$(k8s_node_name_for "$node")"
	socks_port="$(node_listen_port "$node")"

	cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ${deployment_name}
  namespace: ${namespace}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ${deployment_name}
  template:
    metadata:
      labels:
        app: ${deployment_name}
EOF
	print_build_annotations 6
	cat <<EOF
    spec:
EOF
	print_optional_node_name 6 "$node_name"
	cat <<EOF
      containers:
      - name: node
        image: ${runtime_image}
        args:
EOF
	print_yaml_list 10 node --config /etc/tcp-over-kafka/node.json
	cat <<EOF
        ports:
        - name: socks
          containerPort: ${socks_port}
        readinessProbe:
          tcpSocket:
            port: socks
        volumeMounts:
        - name: node-config
          mountPath: /etc/tcp-over-kafka
          readOnly: true
      - name: ssh-target
        image: ${fixture_image}
        args:
EOF
	print_yaml_list 10 sshd
	cat <<EOF
        securityContext:
          capabilities:
            add:
            - SYS_CHROOT
        ports:
        - name: ssh
          containerPort: 22
        readinessProbe:
          tcpSocket:
            port: ssh
        volumeMounts:
        - name: ssh-auth
          mountPath: /etc/tcp-over-kafka-ssh
          readOnly: true
      - name: hello-world-https
        image: ${fixture_image}
        args:
EOF
	print_yaml_list 10 hello-https
	cat <<EOF
        env:
        - name: HELLO_HTTPS_BIND
          value: "0.0.0.0"
        - name: HELLO_HTTPS_PORT
          value: "${HELLO_HTTPS_PORT:-443}"
        - name: HELLO_HTTPS_CERT
          value: "/etc/tcp-over-kafka-hello/server.pem"
        - name: HELLO_HTTPS_KEY
          value: "/etc/tcp-over-kafka-hello/server.key"
        ports:
        - name: https
          containerPort: ${HELLO_HTTPS_PORT:-443}
        readinessProbe:
          tcpSocket:
            port: https
        volumeMounts:
        - name: hello-https
          mountPath: /etc/tcp-over-kafka-hello
          readOnly: true
      volumes:
      - name: node-config
        configMap:
          name: $(k8s_node_config_map_name "$node")
      - name: ssh-auth
        secret:
          secretName: $(k8s_ssh_auth_secret_name)
          defaultMode: 0400
      - name: hello-https
        secret:
          secretName: $(k8s_node_https_secret_name "$node")
          defaultMode: 0400
EOF
}

render_node() {
	local node=$1
	render_ssh_auth_secret "$node"
	render_node_config_map "$node"
	render_node_https_secret "$node"
	render_node_service "$node"
	render_node_deployment "$node"
}

case "$component" in
broker)
	render_namespace "$(k8s_namespace_for broker)"
	render_broker
	;;
node-a)
	render_namespace "$(k8s_namespace_for node-a)"
	render_node node-a
	;;
node-b)
	render_namespace "$(k8s_namespace_for node-b)"
	render_node node-b
	;;
all)
	render_namespace "$(k8s_namespace_for broker)"
	render_broker
	render_namespace "$(k8s_namespace_for node-a)"
	render_node node-a
	render_namespace "$(k8s_namespace_for node-b)"
	render_node node-b
	;;
*)
	die "unknown component: $component"
	;;
esac
