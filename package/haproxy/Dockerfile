FROM ubuntu:26.04 AS haproxy-builder

ARG HAPROXY_VERSION=3.3.8
ARG HAPROXY_SHA256=89b1fe73d54d5990f74997da837f5fd0da1627a1baa62b26f5d358a6f3c48295
ARG UBUNTU_MIRROR=http://archive.ubuntu.com/ubuntu
ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) -exec sed -i \
        -e "s|http://security.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
        -e "s|http://archive.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" {} +; \
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\nAcquire::https::Timeout "30";\nAcquire::http::Pipeline-Depth "0";\n' > /etc/apt/apt.conf.d/80rc16-retries; \
    rm -f /usr/bin/pebble && \
    apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libcap2-bin \
        gcc \
        libc6-dev \
        libcrypt-dev \
        liblua5.4-dev \
        libpcre2-dev \
        libssl-dev \
        make \
        zlib1g-dev && \
    rm -rf /var/lib/apt/lists/*

RUN curl -fsSL -o /tmp/haproxy.tar.gz https://www.haproxy.org/download/3.3/src/haproxy-${HAPROXY_VERSION}.tar.gz && \
    echo "${HAPROXY_SHA256}  /tmp/haproxy.tar.gz" | sha256sum -c - && \
    tar xzf /tmp/haproxy.tar.gz -C /tmp && \
    make -C /tmp/haproxy-${HAPROXY_VERSION} -j"$(nproc)" TARGET=linux-glibc \
      USE_OPENSSL=1 USE_PCRE2=1 USE_PCRE2_JIT=1 USE_ZLIB=1 USE_LUA=1 USE_PROMEX=1 && \
    make -C /tmp/haproxy-${HAPROXY_VERSION} install-bin && \
    rm -rf /tmp/haproxy.tar.gz /tmp/haproxy-${HAPROXY_VERSION}

FROM ubuntu:26.04

ARG UBUNTU_MIRROR=http://archive.ubuntu.com/ubuntu
ARG RC16_IMAGE_VERSION=dev
ENV DEBIAN_FRONTEND=noninteractive

LABEL org.opencontainers.image.source="https://github.com/chen21019/rancher-1.6-lb-controller" \
      org.opencontainers.image.description="Unofficial Rancher 1.6 maintenance HAProxy load balancer image." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${RC16_IMAGE_VERSION}"

RUN set -eux; \
    find /etc/apt -type f \( -name '*.list' -o -name '*.sources' \) -exec sed -i \
        -e "s|http://security.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
        -e "s|http://archive.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" {} +; \
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\nAcquire::https::Timeout "30";\nAcquire::http::Pipeline-Depth "0";\n' > /etc/apt/apt.conf.d/80rc16-retries; \
    rm -f /usr/bin/pebble && \
    apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libcap2-bin \
        jq \
        libcrypt1 \
        liblua5.4-0 \
        libpcre2-8-0 \
        libssl3t64 \
        logrotate \
        openssl \
        rsyslog \
        tini \
        tcpdump \
        vim-tiny \
        wget \
        zlib1g && \
    rm -rf /var/lib/apt/lists/*

COPY --from=haproxy-builder /usr/local/sbin/haproxy /usr/sbin/haproxy

RUN set -eux; \
    groupadd --gid 10001 haproxy; \
    useradd --uid 10001 --gid haproxy --home-dir /var/lib/haproxy --create-home --shell /usr/sbin/nologin haproxy; \
    mkdir -p /etc/haproxy/certs/current /etc/haproxy/certs/new /var/log/haproxy /run/haproxy /var/lib/haproxy; \
    setcap cap_net_bind_service,cap_sys_chroot=+ep /usr/sbin/haproxy; \
    chown -R 10001:10001 /etc/haproxy /var/log/haproxy /run/haproxy /var/lib/haproxy

COPY lb-controller /usr/bin/
COPY scripts/* /usr/bin/
COPY config/* /etc/haproxy/

RUN chown -R 10001:10001 /etc/haproxy

RUN touch /var/log/haproxy/traffic /var/log/haproxy/events /var/log/haproxy/errors && \
    chown -R 10001:10001 /var/log/haproxy && \
    cat /etc/haproxy/logrotate.cfg >> /etc/logrotate.d/haproxy

COPY update-rancher-ssl /usr/bin/
COPY loglevel /usr/bin/
RUN chmod +x /usr/bin/update-rancher-ssl /usr/bin/loglevel

COPY lb-controller.sh /usr/bin/

RUN ln -sf /proc/1/fd/1 /var/log/haproxy/events && \
    ln -sf /proc/1/fd/1 /var/log/haproxy/traffic && \
    ln -sf /proc/1/fd/2 /var/log/haproxy/errors

# So we do not write to the COW filesystem
VOLUME /var/log/haproxy

ENTRYPOINT ["/usr/bin/tini", "-s", "--"]

CMD ["lb-controller.sh", "--controller", "rancher", "--provider",  "haproxy"]

USER 10001:10001
