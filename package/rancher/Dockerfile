FROM ghcr.io/chen21019/rc16-agent-base:v0.3.5

ARG UBUNTU_MIRROR=http://archive.ubuntu.com/ubuntu
ARG RC16_IMAGE_VERSION=dev
ENV DEBIAN_FRONTEND=noninteractive

LABEL org.opencontainers.image.source="https://github.com/chen21019/rancher-1.6-lb-controller" \
      org.opencontainers.image.description="Unofficial Rancher 1.6 maintenance Rancher LB controller sidecar image." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${RC16_IMAGE_VERSION}"

RUN set -eux; \
    find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) -exec sed -i \
        -e "s|http://security.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
        -e "s|http://archive.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" {} +; \
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\nAcquire::https::Timeout "30";\nAcquire::http::Pipeline-Depth "0";\n' > /etc/apt/apt.conf.d/80rc16-retries; \
    rm -f /usr/bin/pebble && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        jq \
        openssl \
        unzip \
        wget && \
    rm -rf /var/lib/apt/lists/*

COPY update-rancher-ssl /usr/bin/
COPY loglevel /usr/bin/
RUN chmod +x /usr/bin/update-rancher-ssl /usr/bin/loglevel

COPY entry.sh /usr/bin/
COPY lb-controller /usr/bin/

ENTRYPOINT ["/usr/bin/entry.sh"]
