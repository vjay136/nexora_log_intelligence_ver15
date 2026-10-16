#!/usr/bin/env bash
set -euo pipefail

ROOT=${ROOT:-/src}
LOCK=${1:-$ROOT/packaging/docker/rpm-packages.lock}
cat >/etc/yum.repos.d/nli-rocky-9.6-vault.repo <<'REPO'
[nli-vault-baseos]
name=NLI Rocky 9.6 BaseOS vault
baseurl=https://dl.rockylinux.org/vault/rocky/9.6/BaseOS/$basearch/os/
enabled=1
gpgcheck=1
repo_gpgcheck=0
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-Rocky-9
metadata_expire=never

[nli-vault-appstream]
name=NLI Rocky 9.6 AppStream vault
baseurl=https://dl.rockylinux.org/vault/rocky/9.6/AppStream/$basearch/os/
enabled=1
gpgcheck=1
repo_gpgcheck=0
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-Rocky-9
metadata_expire=never
REPO
mapfile -t packages < <(tr -d '\r' <"$LOCK")
[[ ${#packages[@]} -gt 0 ]] || { echo "empty package lock: $LOCK" >&2; exit 1; }
dnf -y --disablerepo='*' --enablerepo='nli-vault-*' --setopt=install_weak_deps=False install "${packages[@]}"
