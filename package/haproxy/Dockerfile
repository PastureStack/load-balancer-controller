FROM ubuntu:26.04 AS haproxy-builder

ARG HAPROXY_VERSION=3.3.7
ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gcc \
    libc6-dev \
    libcrypt-dev \
    liblua5.4-dev \
    libpcre2-dev \
    libssl-dev \
    make \
    zlib1g-dev && \
    rm -rf /var/lib/apt/lists/*

RUN curl -sfL https://www.haproxy.org/download/3.3/src/haproxy-${HAPROXY_VERSION}.tar.gz | tar xzf - -C /tmp && \
    make -C /tmp/haproxy-${HAPROXY_VERSION} -j"$(nproc)" TARGET=linux-glibc \
      USE_OPENSSL=1 USE_PCRE2=1 USE_PCRE2_JIT=1 USE_ZLIB=1 USE_LUA=1 USE_PROMEX=1 && \
    make -C /tmp/haproxy-${HAPROXY_VERSION} install-bin

FROM ubuntu:26.04

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    haproxy \
    jq \
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

RUN mkdir -p /etc/haproxy/

COPY lb-controller /usr/bin/
COPY scripts/* /usr/bin/
COPY config/* /etc/haproxy/

RUN mkdir /var/log/haproxy
RUN touch /var/log/haproxy/traffic /var/log/haproxy/events /var/log/haproxy/errors
RUN cat /etc/haproxy/logrotate.cfg >> /etc/logrotate.d/haproxy

COPY update-rancher-ssl /usr/bin/
COPY loglevel /usr/bin/
RUN chmod +x /usr/bin/update-rancher-ssl /usr/bin/loglevel

COPY lb-controller.sh /usr/bin/

RUN ln -sf /proc/1/fd/1 /var/log/haproxy/events
RUN ln -sf /proc/1/fd/1 /var/log/haproxy/traffic
RUN ln -sf /proc/1/fd/2 /var/log/haproxy/errors

# So we do not write to the COW filesystem
VOLUME /var/log/haproxy

ENTRYPOINT ["/usr/bin/tini", "-s", "--"]

CMD ["lb-controller.sh", "--controller", "rancher", "--provider",  "haproxy"]
