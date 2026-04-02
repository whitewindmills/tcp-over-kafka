#!/usr/bin/env bash

# Install CCOS Edge on multiple standalone hosts using the online-install flow.
# The script applies one idempotent command set to each host sequentially and
# validates the result before continuing to the next host.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

readonly CCOSEDGE_IMAGE_DEFAULT="image.cestc.cn/ccos-ceake/ccosedge:tok-poc-260402"
readonly OVN_IMAGE_DEFAULT="image.cestc.cn/ccos-ceake/ovs-app-cclinux220902:test-20240122"
readonly REGISTRY_IP_DEFAULT="10.32.43.15"
readonly SSH_PASSWORD_DEFAULT="cecloud.com"
readonly REMOTE_USER_DEFAULT="root"
readonly REMOTE_HOSTS_DEFAULT="10.253.15.172 10.253.15.173 10.253.15.174"

readonly REGISTRY_CA_PEM='-----BEGIN CERTIFICATE-----
MIIDYzCCAkugAwIBAgIUP24/iLJtwQe7ai0CgW6AoRPXdtMwDQYJKoZIhvcNAQEL
BQAwQTELMAkGA1UEBhMCQ04xEDAOBgNVBAgMB0JlaWppbmcxEDAOBgNVBAcMB0Jl
aWppbmcxDjAMBgNVBAoMBUNlc3RjMB4XDTIxMDUyMzE4NDIyMFoXDTMxMDUyMTE4
NDIyMFowQTELMAkGA1UEBhMCQ04xEDAOBgNVBAgMB0JlaWppbmcxEDAOBgNVBAcM
B0JlaWppbmcxDjAMBgNVBAoMBUNlc3RjMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAxarCwk3bD3Kko/e0rlHB6jnEzD9UQKle97poZyU0h/tTLR/sPndn
SWgd/6JdLDBGWaZKmzCJMlQuU5WRwJzfh1vAwID/5FCxA1SLZGuAD9DgdY1qRs+G
TGEaoh0ch9Jty8n5af3x1w3x6HnK+krJIp/CC6UTVkIaDZaAjMfVsKHYH6J2hkg9
1Dg9kRTcx+ihyc6sPe9T7Nuz6zo6WkBcYJlWQPa2sl8YGMoQcQXmilPw9beESDep
JXmxpgZHCY1CxbNA11zmaYj48+yLgU1Do59pAxzfV0rBYjDdx6UUeVbkWP37WIHt
5AlDe0nUjhVGpVUV9/7IjbKgebZviqCN4QIDAQABo1MwUTAdBgNVHQ4EFgQUuX5o
Hf/zfvLsxWX6emlrtbiZKyEwHwYDVR0jBBgwFoAUuX5oHf/zfvLsxWX6emlrtbiZ
KyEwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEApqCQqVUcs4Re
k9FDkDluhvI1Ck6J+ZVdwSaf15jMm1SazHRI0GAG6FpTjqKy8aI4dZLhRUXfos6J
+KfPJjGgQVMJVxIO6pEstcdpaA8lMNhnnoQZvDXn+EmRwRpYLFMg+O0HWoJmE+eF
mWRy+Kp32thC9zEezAjer9500HrkenrZYU0dn3OP2AFluHnFQCVjdz+D9LSny6u4
jlzYfC52mWVj3kin9HNs6Bj9GJTbDEdb1B1M4Bv8iUmQwg8q0wwT0uGDGAXK0aL7
zaffi5kQWFguy7v+oOKKOMVjX/fKV7FMKEDaB9Lgv+qHTCffn6psFvJkWf+ABuFf
0x0OhhcQQA==
-----END CERTIFICATE-----'

usage() {
	cat <<'EOF'
Usage: hack/ccosedge-online-install-multi-host.sh [all|HOST...]

Environment overrides:
  SSH_PASSWORD      Password for the remote root account.
  REMOTE_USER       SSH user. Defaults to root.
  REMOTE_HOSTS      Space-separated target hosts.
  REGISTRY_IP       Static IP used for image.cestc.cn and yum.cestc.cn.
  CCOSEDGE_IMAGE    CCOS Edge image to install.
  OVN_IMAGE         OVN image to pre-pull.
EOF
}

hostname_for() {
	case "$1" in
	10.253.15.172) printf '%s' "single172" ;;
	10.253.15.173) printf '%s' "single173" ;;
	10.253.15.174) printf '%s' "single174" ;;
	*) die "no hostname mapping defined for host: $1" ;;
	esac
}

setup_ssh() {
	require_command ssh
	require_command scp
	require_command sshpass

	export SSHPASS="${SSH_PASSWORD:-$SSH_PASSWORD_DEFAULT}"
	ssh_cmd=(sshpass -e ssh)
	scp_cmd=(sshpass -e scp)
	ssh_common_opts=(
		-o BatchMode=no
		-o NumberOfPasswordPrompts=1
		-o PreferredAuthentications=password
		-o PubkeyAuthentication=no
		-o StrictHostKeyChecking=no
		-o UserKnownHostsFile=/dev/null
		-o LogLevel=ERROR
	)
	scp_common_opts=(
		-o BatchMode=no
		-o NumberOfPasswordPrompts=1
		-o PreferredAuthentications=password
		-o PubkeyAuthentication=no
		-o StrictHostKeyChecking=no
		-o UserKnownHostsFile=/dev/null
		-o LogLevel=ERROR
	)
}

remote_exec() {
	local remote=$1
	local command=$2
	"${ssh_cmd[@]}" "${ssh_common_opts[@]}" "$remote" "$command"
}

copy_remote_file() {
	local remote=$1
	local src=$2
	local dst=$3
	local mode=$4
	local tmp

	tmp="/tmp/$(basename "$dst").$$"
	"${scp_cmd[@]}" "${scp_common_opts[@]}" "$src" "${remote}:${tmp}" >/dev/null
	remote_exec "$remote" "install -D -m ${mode} '${tmp}' '${dst}' && rm -f '${tmp}'"
}

write_remote_runner() {
	local output=$1

	cat >"${output}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

log() {
	printf '[ccosedge-install] %s\n' "$*" >&2
}

ensure_hosts_alias() {
	local ip=$1
	local name=$2

	if grep -Eq "(^|[[:space:]])${name}([[:space:]]|$)" /etc/hosts; then
		return 0
	fi
	printf '%s %s\n' "${ip}" "${name}" >>/etc/hosts
}

ensure_nameserver() {
	local nameserver=$1

	if grep -qxF "nameserver ${nameserver}" /etc/resolv.conf; then
		return 0
	fi
	printf '%s\n' "nameserver ${nameserver}" >>/etc/resolv.conf
}

write_updates_repo() {
	cat >/etc/yum.repos.d/cclinux-updates.repo <<REPO
[baseos-updates]
name=CCLinux \$releasever - BaseOS-updates
baseurl=http://yum.cestc.cn/repo/cclinux/22.09.2-updates/BaseOS/\$basearch/os/
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-cclinuxofficial
gpgcheck=1
repo_gpgcheck=0
metadata_expire=6h
countme=1
enabled=1

[appstream-updates]
name=CCLinux \$releasever - AppStream-updates
baseurl=http://yum.cestc.cn/repo/cclinux/22.09.2-updates/AppStream/\$basearch/os/
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-cclinuxofficial
gpgcheck=1
repo_gpgcheck=0
metadata_expire=6h
countme=1
enabled=1

[crb-updates]
name=CCLinux \$releasever - CRB-updates
baseurl=http://yum.cestc.cn/repo/cclinux/22.09.2-updates/CRB/\$basearch/os/
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-cclinuxofficial
gpgcheck=1
repo_gpgcheck=0
metadata_expire=6h
countme=1
enabled=1

[epel-updates]
name=CCLinux \$releasever - epel-updates
baseurl=http://yum.cestc.cn/repo/cclinux/22.09.2-updates/epel/9/Everything/\$basearch/
#gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-cclinuxofficial
gpgcheck=1
repo_gpgcheck=0
metadata_expire=6h
countme=1
enabled=1

[devel-updates]
name=CCLinux \$releasever - devel-updates
baseurl=http://yum.cestc.cn/repo/cclinux/22.09.2-updates/Devel/\$basearch/os/
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-cclinuxofficial
gpgcheck=1
repo_gpgcheck=0
metadata_expire=6h
countme=1
enabled=1
REPO
}

write_registry_ca() {
	install -d /etc/pki/ca-trust/source/anchors
	cat >/etc/pki/ca-trust/source/anchors/rootca.crt <<'CRT'
__REGISTRY_CA_PEM__
CRT

	# Podman can read system trust, but writing the same anchor into the
	# registry-specific path keeps pulls working even if trust extraction lags.
	install -d /etc/containers/certs.d/image.cestc.cn
	cp /etc/pki/ca-trust/source/anchors/rootca.crt /etc/containers/certs.d/image.cestc.cn/ca.crt
	update-ca-trust
}

write_registry_mirror() {
	install -d /etc/containers/registries.conf.d
	cat >/etc/containers/registries.conf.d/004-ccosedge-mirror-online-registry.conf <<'CONF'
[[registry]]
prefix = "image.ccos.io"
location = "image.ccos.io"

  [[registry.mirror]]
  location = "image.cestc.cn"
  pull-from-mirror = "tag-only"
CONF
}

install_runtime() {
	if ! yum makecache; then
		log "updates repo is unavailable, disabling /etc/yum.repos.d/cclinux-updates.repo and retrying"
		sed -i 's/^enabled=1$/enabled=0/' /etc/yum.repos.d/cclinux-updates.repo || true
		yum clean all
		yum makecache
	fi
	yum install -y crio podman
	systemctl enable --now crio
}

prepare_runtime_files() {
	install -d /etc/crio
	printf '{}\n' >/etc/crio/ccos-pull-secret
	write_registry_mirror
	systemctl restart crio
}

run_install_image() {
	local image=$1
	local mount_dir install_script

	mount_dir="$(podman image mount "${image}")"
	trap 'podman image unmount "${image}" >/dev/null 2>&1 || true' RETURN
	install_script="${mount_dir}/app/scripts/install.sh"
	if [ ! -x "${install_script}" ]; then
		install_script="$(find "${mount_dir}" -path '*/app/scripts/install.sh' -o -path '*/scripts/install.sh' | head -n 1)"
	fi
	[ -n "${install_script}" ] || {
		log "install.sh not found inside ${image}"
		exit 1
	}

	if command -v timeout >/dev/null 2>&1; then
		if ! timeout "${INSTALL_TIMEOUT_SECONDS:-900}" bash "${install_script}"; then
			log "install.sh did not complete cleanly within ${INSTALL_TIMEOUT_SECONDS:-900}s"
			systemctl status --no-pager ccos-cli-init.service || true
			journalctl -u ccos-cli-init.service -n 80 --no-pager || true
			systemctl stop ccos-cli-init.service >/dev/null 2>&1 || true
			systemctl reset-failed ccos-cli-init.service >/dev/null 2>&1 || true
			exit 1
		fi
	else
		bash "${install_script}"
	fi

	podman image unmount "${image}" >/dev/null
	trap - RETURN
}

validate_host() {
	local ovn_image=$1
	local ccosedge_image=$2

	hostnamectl --static
	getent hosts image.cestc.cn yum.cestc.cn
	rpm -q cri-o podman
	systemctl is-active crio
	podman pull "${ovn_image}" >/dev/null
	podman pull "${ccosedge_image}" >/dev/null
	systemctl --failed
	if systemctl list-unit-files | grep -q '^ccos-cli-init.service'; then
		systemctl status --no-pager ccos-cli-init.service
	fi
}

main() {
	local target_hostname=$1
	local registry_ip=$2
	local ovn_image=$3
	local ccosedge_image=$4

	log "setting hostname to ${target_hostname}"
	hostnamectl set-hostname "${target_hostname}"

	log "writing static name resolution"
	ensure_hosts_alias "${registry_ip}" image.cestc.cn
	ensure_hosts_alias "${registry_ip}" yum.cestc.cn
	ensure_nameserver 8.8.8.8

	log "writing yum updates repo and registry trust"
	write_updates_repo
	write_registry_ca

	log "installing container runtime"
	install_runtime
	prepare_runtime_files

	log "pre-pulling required images"
	podman pull "${ovn_image}"
	podman pull "${ccosedge_image}"

	log "running CCOS Edge install.sh from ${ccosedge_image}"
	run_install_image "${ccosedge_image}"

	log "validating host state"
	validate_host "${ovn_image}" "${ccosedge_image}"
}

main "$@"
EOF

	sed -i "s|__REGISTRY_CA_PEM__|${REGISTRY_CA_PEM//$'\n'/\\n}|g" "${output}"
}

install_one_host() {
	local host=$1
	local remote_user=${REMOTE_USER:-$REMOTE_USER_DEFAULT}
	local registry_ip=${REGISTRY_IP:-$REGISTRY_IP_DEFAULT}
	local ovn_image=${OVN_IMAGE:-$OVN_IMAGE_DEFAULT}
	local ccosedge_image=${CCOSEDGE_IMAGE:-$CCOSEDGE_IMAGE_DEFAULT}
	local hostname_target
	local remote
	local runner

	hostname_target="$(hostname_for "$host")"
	remote="${remote_user}@${host}"
	runner="$(mktemp)"
	write_remote_runner "${runner}"
	chmod 0755 "${runner}"

	log "installing CCOS Edge on ${host} as ${hostname_target}"
	copy_remote_file "${remote}" "${runner}" "/usr/local/bin/ccosedge-online-install.sh" 0755
	rm -f "${runner}"
	remote_exec "${remote}" "/usr/local/bin/ccosedge-online-install.sh '${hostname_target}' '${registry_ip}' '${ovn_image}' '${ccosedge_image}'"
}

main() {
	local -a targets=()
	local arg

	if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
		usage
		exit 0
	fi

	setup_ssh

	if [ "$#" -eq 0 ] || [ "${1:-}" = "all" ]; then
		read -r -a targets <<<"${REMOTE_HOSTS:-$REMOTE_HOSTS_DEFAULT}"
	else
		for arg in "$@"; do
			targets+=("${arg}")
		done
	fi

	for arg in "${targets[@]}"; do
		install_one_host "${arg}"
	done
}

main "$@"
