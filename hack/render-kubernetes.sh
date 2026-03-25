#!/usr/bin/env bash

# Render kubernetes manifests for the broker, client, and server components.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

component="${1:-}"
[ -n "$component" ] || die "usage: render-kubernetes.sh <broker|client|server|all>"

load_local_env

namespace="${KUBERNETES_NAMESPACE:-tcp-over-kafka}"
remote_bin_dir="${REMOTE_BIN_DIR:-/usr/local/bin}"
remote_binary_path="${remote_bin_dir}/tcp-over-kafka"
tunnel_runtime_image="${TUNNEL_RUNTIME_IMAGE:-gcr.io/distroless/static-debian12}"
hello_https_container_image="${HELLO_HTTPS_CONTAINER_IMAGE:-python:3.12-slim}"

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

# render_namespace emits the namespace manifest used by all resources.
render_namespace() {
	cat <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
EOF
}

# render_broker emits a broker deployment with host networking and a hostPath volume.
render_broker() {
	local broker_host broker_port node_name
	broker_host=
	broker_port=
	split_host_port "${BROKER_ADDR}" broker_host broker_port
	node_name="$(k8s_node_name_for broker)"

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
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
EOF
	print_optional_node_name 6 "$node_name"
	cat <<EOF
      containers:
      - name: broker
        image: ${BROKER_IMAGE}
        args:
EOF
	print_yaml_list 10 \
		redpanda \
		start \
		--mode dev-container \
		--check=false \
		--node-id 0 \
		--kafka-addr "internal://0.0.0.0:${broker_port},external://0.0.0.0:${broker_port}" \
		--advertise-kafka-addr "internal://127.0.0.1:${broker_port},external://${broker_host}:${broker_port}" \
		--rpc-addr "0.0.0.0:${BROKER_RPC_PORT:-33145}" \
		--advertise-rpc-addr "${broker_host}:${BROKER_RPC_PORT:-33145}"
	cat <<EOF
        volumeMounts:
        - name: broker-data
          mountPath: /var/lib/redpanda/data
      volumes:
      - name: broker-data
        hostPath:
          path: ${BROKER_DATA_DIR}
          type: DirectoryOrCreate
EOF
}

# render_client emits a client deployment that runs the host-installed tunnel binary.
render_client() {
	local client_args=()
	local node_name
	build_client_args client_args
	node_name="$(k8s_node_name_for client)"

	cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tcp-over-kafka-client
  namespace: ${namespace}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tcp-over-kafka-client
  template:
    metadata:
      labels:
        app: tcp-over-kafka-client
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
EOF
	print_optional_node_name 6 "$node_name"
	cat <<EOF
      containers:
      - name: client
        image: ${tunnel_runtime_image}
        command:
EOF
	print_yaml_list 10 "${remote_binary_path}"
	cat <<EOF
        args:
EOF
	print_yaml_list 10 "${client_args[@]}"
	cat <<EOF
        volumeMounts:
        - name: tunnel-binary
          mountPath: ${remote_binary_path}
          readOnly: true
      volumes:
      - name: tunnel-binary
        hostPath:
          path: ${remote_binary_path}
          type: File
EOF
}

# render_server emits a server deployment with a sidecar HTTPS helper.
render_server() {
	local server_args=()
	local node_name
	build_server_args server_args
	node_name="$(k8s_node_name_for server)"

	cat <<EOF
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: tcp-over-kafka-hello-script
  namespace: ${namespace}
data:
  hello_https_server.py: |
EOF
	sed 's/^/    /' "${SCRIPT_DIR}/hello_https_server.py"
	cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tcp-over-kafka-server
  namespace: ${namespace}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tcp-over-kafka-server
  template:
    metadata:
      labels:
        app: tcp-over-kafka-server
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
EOF
	print_optional_node_name 6 "$node_name"
	cat <<EOF
      containers:
      - name: server
        image: ${tunnel_runtime_image}
        command:
EOF
	print_yaml_list 10 "${remote_binary_path}"
	cat <<EOF
        args:
EOF
	print_yaml_list 10 "${server_args[@]}"
	cat <<EOF
        volumeMounts:
        - name: tunnel-binary
          mountPath: ${remote_binary_path}
          readOnly: true
      - name: hello-world-https
        image: ${hello_https_container_image}
        command:
EOF
	print_yaml_list 10 python /opt/tcp-over-kafka/hello_https_server.py
	cat <<EOF
        args:
EOF
	print_yaml_list 10 \
		--bind "${HELLO_HTTPS_BIND:-0.0.0.0}" \
		--port "${HELLO_HTTPS_PORT:-443}" \
		--cert "${HELLO_HTTPS_CERT}" \
		--key "${HELLO_HTTPS_KEY}"
	cat <<EOF
        volumeMounts:
        - name: hello-script
          mountPath: /opt/tcp-over-kafka
          readOnly: true
        - name: hello-cert
          mountPath: ${HELLO_HTTPS_CERT}
          readOnly: true
        - name: hello-key
          mountPath: ${HELLO_HTTPS_KEY}
          readOnly: true
      volumes:
      - name: tunnel-binary
        hostPath:
          path: ${remote_binary_path}
          type: File
      - name: hello-script
        configMap:
          name: tcp-over-kafka-hello-script
      - name: hello-cert
        hostPath:
          path: ${HELLO_HTTPS_CERT}
          type: File
      - name: hello-key
        hostPath:
          path: ${HELLO_HTTPS_KEY}
          type: File
EOF
}

render_namespace
case "$component" in
broker)
	render_broker
	;;
client)
	render_client
	;;
server)
	render_server
	;;
all)
	render_broker
	render_client
	render_server
	;;
*)
	die "unknown component: $component"
	;;
esac
