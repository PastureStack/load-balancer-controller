FROM ubuntu:26.04@sha256:678c6550cc43645e08669028bc177f50be4e7c5b8cca677067b1914d4afc7a03 AS trust-bootstrap

ADD --checksum=sha256:6077d27c6b6f8b23590cb01ff877ed8c804a67a5442cc32b5a33da10d2bd0e90 https://snapshot.ubuntu.com/ubuntu/20260814T000000Z/pool/main/c/ca-certificates/ca-certificates_20260601~26.04.1_all.deb /tmp/ca-certificates.deb

RUN set -eux; \
    rm -rf /tmp/ca-bootstrap; \
    mkdir -p /tmp/ca-bootstrap; \
    dpkg-deb --extract /tmp/ca-certificates.deb /tmp/ca-bootstrap; \
    find /tmp/ca-bootstrap/usr/share/ca-certificates -type f -name '*.crt' \
        | LC_ALL=C sort \
        | while IFS= read -r certificate; do cat "${certificate}"; printf '\n'; done \
        > /ca-certificates.crt; \
    test -s /ca-certificates.crt

FROM ubuntu:26.04@sha256:678c6550cc43645e08669028bc177f50be4e7c5b8cca677067b1914d4afc7a03 AS haproxy-builder

COPY --from=trust-bootstrap /ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY ubuntu-apt.lock /licenses/ubuntu-apt.lock

ARG HAPROXY_VERSION=3.4.3
ARG HAPROXY_SERIES=3.4
ARG HAPROXY_SHA256=7fa666d36d198275999e2a68dda44d3d37960f2f7aed3a595fb811f4fd0515b5
ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    . /licenses/ubuntu-apt.lock; \
    test -s /etc/ssl/certs/ca-certificates.crt; \
    test -s /usr/share/keyrings/ubuntu-archive-keyring.gpg; \
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: resolute resolute-updates resolute-backports resolute-security\nComponents: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\nSnapshot: no\n' \
        "${UBUNTU_APT_SNAPSHOT}" > /etc/apt/sources.list.d/pasturestack-snapshot.sources; \
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\nAcquire::https::Timeout "30";\nAcquire::http::Pipeline-Depth "0";\nAcquire::https::CaInfo "/etc/ssl/certs/ca-certificates.crt";\nAcquire::https::Verify-Peer "true";\nAcquire::https::Verify-Host "true";\nAcquire::AllowInsecureRepositories "false";\nAPT::Get::AllowUnauthenticated "false";\n' > /etc/apt/apt.conf.d/80pasturestack-retries; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates="${UBUNTU_APT_CA_CERTIFICATES_VERSION}" \
        curl="${UBUNTU_APT_CURL_VERSION}" \
        gcc="${UBUNTU_APT_GCC_VERSION}" \
        libc6-dev="${UBUNTU_APT_LIBC6_DEV_VERSION}" \
        libcap2-bin="${UBUNTU_APT_LIBCAP2_BIN_VERSION}" \
        libcrypt-dev="${UBUNTU_APT_LIBCRYPT_DEV_VERSION}" \
        liblua5.4-dev="${UBUNTU_APT_LIBLUA5_4_DEV_VERSION}" \
        libpcre2-dev="${UBUNTU_APT_LIBPCRE2_DEV_VERSION}" \
        libssl-dev="${UBUNTU_APT_LIBSSL_DEV_VERSION}" \
        make="${UBUNTU_APT_MAKE_VERSION}" \
        zlib1g-dev="${UBUNTU_APT_ZLIB1G_DEV_VERSION}"; \
    { \
        printf 'snapshot\t%s\n' "${UBUNTU_APT_SNAPSHOT}"; \
        dpkg-query -W -f='${binary:Package}\t${Version}\n' | LC_ALL=C sort; \
    } > /licenses/HAPROXY-BUILDER-UBUNTU-APT-PACKAGES.tsv; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/*; \
    rm -f /usr/bin/pebble

RUN curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 300 \
        -o /tmp/haproxy.tar.gz https://www.haproxy.org/download/${HAPROXY_SERIES}/src/haproxy-${HAPROXY_VERSION}.tar.gz && \
    echo "${HAPROXY_SHA256}  /tmp/haproxy.tar.gz" | sha256sum -c - && \
    tar xzf /tmp/haproxy.tar.gz -C /tmp && \
    make -C /tmp/haproxy-${HAPROXY_VERSION} -j"$(nproc)" TARGET=linux-glibc \
      USE_OPENSSL=1 USE_PCRE2=1 USE_PCRE2_JIT=1 USE_ZLIB=1 USE_LUA=1 USE_PROMEX=1 && \
    make -C /tmp/haproxy-${HAPROXY_VERSION} install-bin && \
    mkdir -p /usr/local/share/haproxy/errors && \
    cp -a /tmp/haproxy-${HAPROXY_VERSION}/examples/errorfiles/*.http /usr/local/share/haproxy/errors/ && \
    rm -rf /tmp/haproxy.tar.gz /tmp/haproxy-${HAPROXY_VERSION}

FROM ubuntu:26.04@sha256:678c6550cc43645e08669028bc177f50be4e7c5b8cca677067b1914d4afc7a03

COPY --from=trust-bootstrap /ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY ubuntu-apt.lock /licenses/ubuntu-apt.lock

ARG PASTURESTACK_IMAGE_VERSION=dev
ARG SOURCE_REVISION=unknown
ENV DEBIAN_FRONTEND=noninteractive

LABEL org.opencontainers.image.source="https://github.com/PastureStack/load-balancer-controller" \
      org.opencontainers.image.description="PastureStack HAProxy load-balancer service compatibility image." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${PASTURESTACK_IMAGE_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"

RUN set -eux; \
    . /licenses/ubuntu-apt.lock; \
    test -s /etc/ssl/certs/ca-certificates.crt; \
    test -s /usr/share/keyrings/ubuntu-archive-keyring.gpg; \
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: resolute resolute-updates resolute-backports resolute-security\nComponents: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\nSnapshot: no\n' \
        "${UBUNTU_APT_SNAPSHOT}" > /etc/apt/sources.list.d/pasturestack-snapshot.sources; \
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\nAcquire::https::Timeout "30";\nAcquire::http::Pipeline-Depth "0";\nAcquire::https::CaInfo "/etc/ssl/certs/ca-certificates.crt";\nAcquire::https::Verify-Peer "true";\nAcquire::https::Verify-Host "true";\nAcquire::AllowInsecureRepositories "false";\nAPT::Get::AllowUnauthenticated "false";\n' > /etc/apt/apt.conf.d/80pasturestack-retries; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates="${UBUNTU_APT_CA_CERTIFICATES_VERSION}" \
        curl="${UBUNTU_APT_CURL_VERSION}" \
        jq="${UBUNTU_APT_JQ_VERSION}" \
        libcap2-bin="${UBUNTU_APT_LIBCAP2_BIN_VERSION}" \
        libcrypt1="${UBUNTU_APT_LIBCRYPT1_VERSION}" \
        liblua5.4-0="${UBUNTU_APT_LIBLUA5_4_0_VERSION}" \
        libpcre2-8-0="${UBUNTU_APT_LIBPCRE2_8_0_VERSION}" \
        libssl3t64="${UBUNTU_APT_LIBSSL3T64_VERSION}" \
        logrotate="${UBUNTU_APT_LOGROTATE_VERSION}" \
        openssl="${UBUNTU_APT_OPENSSL_VERSION}" \
        rsyslog="${UBUNTU_APT_RSYSLOG_VERSION}" \
        tcpdump="${UBUNTU_APT_TCPDUMP_VERSION}" \
        tini="${UBUNTU_APT_TINI_VERSION}" \
        vim-tiny="${UBUNTU_APT_VIM_TINY_VERSION}" \
        wget="${UBUNTU_APT_WGET_VERSION}" \
        zlib1g="${UBUNTU_APT_ZLIB1G_VERSION}"; \
    { \
        printf 'snapshot\t%s\n' "${UBUNTU_APT_SNAPSHOT}"; \
        dpkg-query -W -f='${binary:Package}\t${Version}\n' | LC_ALL=C sort; \
    } > /licenses/HAPROXY-RUNTIME-UBUNTU-APT-PACKAGES.tsv; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/*; \
    rm -f /usr/bin/pebble

COPY --from=haproxy-builder /usr/local/sbin/haproxy /usr/sbin/haproxy
COPY --from=haproxy-builder /usr/local/share/haproxy/errors /etc/haproxy/errors

RUN set -eux; \
    groupadd --gid 10001 haproxy; \
    useradd --uid 10001 --gid haproxy --home-dir /var/lib/haproxy --create-home --shell /usr/sbin/nologin haproxy; \
    mkdir -p /etc/haproxy/certs/current /etc/haproxy/certs/new /var/log/haproxy /run/haproxy /var/lib/haproxy; \
    setcap cap_net_bind_service,cap_sys_chroot=+ep /usr/sbin/haproxy; \
    chown -R 10001:10001 /etc/haproxy /var/log/haproxy /run/haproxy /var/lib/haproxy

COPY lb-controller /usr/bin/
COPY scripts/* /usr/bin/
COPY config/* /etc/haproxy/
COPY LICENSE ORIGIN.md /usr/share/licenses/load-balancer-controller/
COPY third-party-licenses /usr/share/licenses/load-balancer-controller/third-party/

RUN set -eux; \
    install -o root -g root -m 0644 \
        /etc/haproxy/rsyslogd.conf \
        /etc/rsyslog.d/49-pasturestack-haproxy.conf; \
    rm -f /etc/haproxy/rsyslogd.conf; \
    for code in 400 403 408 500 502 503 504; do \
        test -s "/etc/haproxy/errors/${code}.http"; \
    done; \
    test -s /usr/share/licenses/load-balancer-controller/LICENSE; \
    test -s /usr/share/licenses/load-balancer-controller/ORIGIN.md; \
    test -s /usr/share/licenses/load-balancer-controller/third-party/manifest.tsv; \
    test -r /etc/rsyslog.d/49-pasturestack-haproxy.conf; \
    chown -R 10001:10001 /etc/haproxy

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
