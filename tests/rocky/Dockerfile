ARG BASE_IMAGE=quay.io/rockylinux/rockylinux@sha256:91bbb8eb52ca462611c1f9ce5c4cede4172a31bfe64f336e82f29648694a3cfe
FROM ${BASE_IMAGE}
RUN dnf -y --setopt=install_weak_deps=False install \
      systemd systemd-udev procps-ng diffutils ca-certificates \
    && dnf clean all \
    && systemctl mask systemd-remount-fs.service dev-hugepages.mount sys-fs-fuse-connections.mount
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
