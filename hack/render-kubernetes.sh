#!/usr/bin/env bash

# Render kubernetes manifests for the broker and symmetric node components.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

component="${1:-}"
[ -n "$component" ] || die "usage: render-kubernetes.sh <broker|node-a|node-b|all>"

load_local_env

namespace="${KUBERNETES_NAMESPACE:-tcp-over-kafka}"
remote_bin_dir="${REMOTE_BIN_DIR:-/usr/local/bin}"
remote_binary_path="${remote_bin_dir}/tcp-over-kafka"
remote_node_config_path="$(node_config_path_for node-a)"
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

# print_required_node_separation keeps the two host-networked node deployments
# off the same Kubernetes worker so they do not fight over identical ports.
print_required_node_separation() {
	local indent=$1
	printf "%${indent}saffinity:\n" ""
	printf "%$((indent + 2))spodAntiAffinity:\n" ""
	printf "%$((indent + 4))srequiredDuringSchedulingIgnoredDuringExecution:\n" ""
	printf "%$((indent + 6))s- labelSelector:\n" ""
	printf "%$((indent + 8))smatchExpressions:\n" ""
	printf "%$((indent + 10))s- key: app\n" ""
	printf "%$((indent + 12))soperator: In\n" ""
	printf "%$((indent + 12))svalues:\n" ""
	printf "%$((indent + 14))s- tcp-over-kafka-node-a\n" ""
	printf "%$((indent + 14))s- tcp-over-kafka-node-b\n" ""
	printf "%$((indent + 6))stopologyKey: kubernetes.io/hostname\n" ""
}

render_namespace() {
	cat <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
EOF
}

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

render_node() {
	local node=$1
	local deployment_name
	local node_name
	deployment_name="tcp-over-kafka-${node}"
	node_name="$(k8s_node_name_for "$node")"

	cat <<EOF
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ${deployment_name}-hello-script
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
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
EOF
	print_optional_node_name 6 "$node_name"
	print_required_node_separation 6
	cat <<EOF
      containers:
      - name: node
        image: ${tunnel_runtime_image}
        command:
EOF
	print_yaml_list 10 "${remote_binary_path}"
	cat <<EOF
        args:
EOF
	print_yaml_list 10 node --config "${remote_node_config_path}"
	cat <<EOF
        volumeMounts:
        - name: tunnel-binary
          mountPath: ${remote_binary_path}
          readOnly: true
        - name: node-config
          mountPath: ${remote_node_config_path}
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
      - name: node-config
        hostPath:
          path: ${remote_node_config_path}
          type: File
      - name: hello-script
        configMap:
          name: ${deployment_name}-hello-script
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

case "$component" in
broker)
	render_namespace
	render_broker
	;;
node-a)
	render_namespace
	render_node node-a
	;;
node-b)
	render_namespace
	render_node node-b
	;;
all)
	render_namespace
	render_broker
	render_node node-a
	render_node node-b
	;;
*)
	die "unknown component: $component"
	;;
esac
