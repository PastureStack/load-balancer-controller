FROM ghcr.io/pasturestack/node-agent-base:v0.3.5@sha256:d6f958c94e9ac62518759348ab3e74e5d5cbf1d64d4ad390f300d1b51d46bb7b

ARG UBUNTU_MIRROR=http://archive.ubuntu.com/ubuntu
ARG RC16_IMAGE_VERSION=dev
ARG SOURCE_REVISION=unknown
ENV DEBIAN_FRONTEND=noninteractive

LABEL org.opencontainers.image.source="https://github.com/PastureStack/load-balancer-controller" \
      org.opencontainers.image.description="PastureStack load-balancer controller compatibility sidecar image." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${RC16_IMAGE_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"

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
