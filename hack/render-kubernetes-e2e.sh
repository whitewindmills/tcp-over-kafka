#!/usr/bin/env bash

# Render the Kubernetes pod used to execute the shell validation suite inside the cluster network.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

local_node="${1:-}"
[ -n "${local_node}" ] || die "usage: render-kubernetes-e2e.sh <node-a|node-b>"

load_local_env
require_kubernetes_component_prereqs "${local_node}"

namespace="$(k8s_namespace_for "${local_node}")"
runner_name="$(k8s_e2e_runner_name "${local_node}")"

cat <<EOF
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace}
---
apiVersion: v1
kind: Pod
metadata:
  name: ${runner_name}
  namespace: ${namespace}
  labels:
    app: ${runner_name}
    tcp-over-kafka/local-node: ${local_node}
spec:
  restartPolicy: Always
  containers:
  - name: runner
    image: $(tunnel_fixture_image)
    args:
    - "toolbox"
    volumeMounts:
    - name: ssh-auth
      mountPath: /etc/tcp-over-kafka-ssh
      readOnly: true
  volumes:
  - name: ssh-auth
    secret:
      secretName: $(k8s_ssh_auth_secret_name)
      defaultMode: 0400
EOF
