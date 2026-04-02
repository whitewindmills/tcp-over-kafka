#!/usr/bin/env bash

# Entrypoint for the Kubernetes fixture image used by SSH/HTTPS targets and the E2E runner.

set -euo pipefail

mode="${1:-toolbox}"
if [ "$#" -gt 0 ]; then
	shift
fi

configure_sshd() {
	local password_auth pubkey_auth password_file auth_keys_file

	mkdir -p /root/.ssh /var/run/sshd
	chmod 0700 /root/.ssh

	auth_keys_file="/etc/tcp-over-kafka-ssh/authorized_keys"
	if [ -f "${auth_keys_file}" ]; then
		install -m 0600 "${auth_keys_file}" /root/.ssh/authorized_keys
		pubkey_auth=yes
	else
		rm -f /root/.ssh/authorized_keys
		pubkey_auth=no
	fi

	password_file="/etc/tcp-over-kafka-ssh/password"
	if [ -f "${password_file}" ]; then
		printf 'root:%s\n' "$(tr -d '\r\n' <"${password_file}")" | chpasswd
		password_auth=yes
	else
		password_auth=no
	fi

	ssh-keygen -A >/dev/null
	cat >/etc/ssh/sshd_config.d/tcp-over-kafka.conf <<EOF
Port 22
PermitRootLogin yes
PubkeyAuthentication ${pubkey_auth}
PasswordAuthentication ${password_auth}
AuthorizedKeysFile /root/.ssh/authorized_keys
ChallengeResponseAuthentication no
UsePAM no
X11Forwarding no
PermitEmptyPasswords no
EOF
	exec /usr/sbin/sshd -D -e
}

run_hello_https() {
	exec python3 /opt/tcp-over-kafka/hello_https_server.py \
		--bind "${HELLO_HTTPS_BIND:-0.0.0.0}" \
		--port "${HELLO_HTTPS_PORT:-443}" \
		--cert "${HELLO_HTTPS_CERT:-/etc/tcp-over-kafka-hello/server.pem}" \
		--key "${HELLO_HTTPS_KEY:-/etc/tcp-over-kafka-hello/server.key}" \
		"$@"
}

case "${mode}" in
sshd)
	configure_sshd
	;;
hello-https)
	run_hello_https "$@"
	;;
toolbox)
	exec sleep infinity
	;;
*)
	exec "${mode}" "$@"
	;;
esac
