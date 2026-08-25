ARG ALPINE_IMAGE=docker.io/library/alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

FROM ${ALPINE_IMAGE} AS haproxy-builder

COPY alpine-apk.lock /licenses/alpine-apk.lock

ARG HAPROXY_VERSION=3.4.3
ARG HAPROXY_SERIES=3.4
ARG HAPROXY_SHA256=7fa666d36d198275999e2a68dda44d3d37960f2f7aed3a595fb811f4fd0515b5

RUN set -eux; \
    . /licenses/alpine-apk.lock; \
    apk add --no-cache \
        "build-base=${ALPINE_BUILD_BASE_VERSION}" \
        "ca-certificates=${ALPINE_CA_CERTIFICATES_VERSION}" \
        "curl=${ALPINE_CURL_VERSION}" \
        "linux-headers=${ALPINE_LINUX_HEADERS_VERSION}" \
        "lua5.4-dev=${ALPINE_LUA5_4_DEV_VERSION}" \
        "openssl-dev=${ALPINE_OPENSSL_DEV_VERSION}" \
        "pcre2-dev=${ALPINE_PCRE2_DEV_VERSION}" \
        "zlib-dev=${ALPINE_ZLIB_DEV_VERSION}"; \
    curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 300 \
        -o /tmp/haproxy.tar.gz "https://www.haproxy.org/download/${HAPROXY_SERIES}/src/haproxy-${HAPROXY_VERSION}.tar.gz"; \
    echo "${HAPROXY_SHA256}  /tmp/haproxy.tar.gz" | sha256sum -c -; \
    tar xzf /tmp/haproxy.tar.gz -C /tmp; \
    make -C "/tmp/haproxy-${HAPROXY_VERSION}" -j"$(nproc)" \
        TARGET=linux-musl \
        USE_OPENSSL=1 \
        USE_PCRE2=1 \
        USE_PCRE2_JIT=1 \
        USE_ZLIB=1 \
        USE_LUA=1 \
        USE_PROMEX=1 \
        LUA_INC=/usr/include/lua5.4 \
        LUA_LIB=/usr/lib/lua5.4; \
    make -C "/tmp/haproxy-${HAPROXY_VERSION}" install-bin; \
    mkdir -p /usr/local/share/haproxy/errors; \
    cp -a "/tmp/haproxy-${HAPROXY_VERSION}"/examples/errorfiles/*.http /usr/local/share/haproxy/errors/

FROM ${ALPINE_IMAGE}

COPY alpine-apk.lock /licenses/alpine-apk.lock

ARG PASTURESTACK_IMAGE_VERSION=dev
ARG SOURCE_REVISION=unknown

LABEL org.opencontainers.image.source="https://github.com/PastureStack/load-balancer-controller" \
      org.opencontainers.image.description="PastureStack HAProxy load-balancer service compatibility image." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${PASTURESTACK_IMAGE_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"

RUN set -eux; \
    . /licenses/alpine-apk.lock; \
    apk add --no-cache \
        "bash=${ALPINE_BASH_VERSION}" \
        "ca-certificates=${ALPINE_CA_CERTIFICATES_VERSION}" \
        "curl=${ALPINE_CURL_VERSION}" \
        "libcap-utils=${ALPINE_LIBCAP_UTILS_VERSION}" \
        "lua5.4-libs=${ALPINE_LUA5_4_LIBS_VERSION}" \
        "pcre2=${ALPINE_PCRE2_VERSION}" \
        "procps-ng=${ALPINE_PROCPS_NG_VERSION}" \
        "rsyslog=${ALPINE_RSYSLOG_VERSION}" \
        "su-exec=${ALPINE_SU_EXEC_VERSION}" \
        "tini=${ALPINE_TINI_VERSION}"; \
    { \
        printf 'repository\thttps://dl-cdn.alpinelinux.org/alpine/v3.24/main\n'; \
        apk info -v | LC_ALL=C sort; \
    } > /licenses/HAPROXY-RUNTIME-ALPINE-APK-PACKAGES.tsv

COPY --from=haproxy-builder /usr/local/sbin/haproxy /usr/sbin/haproxy
COPY --from=haproxy-builder /usr/local/share/haproxy/errors /etc/haproxy/errors

RUN set -eux; \
    addgroup -g 10001 -S haproxy; \
    adduser -u 10001 -S -D -H -h /var/lib/haproxy -s /sbin/nologin -G haproxy haproxy; \
    mkdir -p /etc/haproxy/certs/current /etc/haproxy/certs/new /etc/rsyslog.d /var/log/haproxy /run/haproxy /var/lib/haproxy; \
    setcap cap_net_bind_service,cap_sys_chroot=+ep /usr/sbin/haproxy; \
    chown -R 10001:10001 /etc/haproxy /var/log/haproxy /run/haproxy /var/lib/haproxy

COPY lb-controller /usr/bin/
COPY scripts/* /usr/bin/
COPY config/* /etc/haproxy/
COPY update-rancher-ssl /usr/bin/
COPY loglevel /usr/bin/
COPY lb-controller.sh /usr/bin/
COPY LICENSE ORIGIN.md /usr/share/licenses/load-balancer-controller/
COPY third-party-licenses /usr/share/licenses/load-balancer-controller/third-party/

RUN set -eux; \
    install -o root -g root -m 0644 \
        /etc/haproxy/rsyslogd.conf \
        /etc/rsyslog.d/49-pasturestack-haproxy.conf; \
    rm -f /etc/haproxy/rsyslogd.conf /etc/haproxy/logrotate.cfg; \
    for code in 400 403 408 500 502 503 504; do \
        test -s "/etc/haproxy/errors/${code}.http"; \
    done; \
    test -s /usr/share/licenses/load-balancer-controller/LICENSE; \
    test -s /usr/share/licenses/load-balancer-controller/ORIGIN.md; \
    test -s /usr/share/licenses/load-balancer-controller/third-party/manifest.tsv; \
    test -r /etc/rsyslog.d/49-pasturestack-haproxy.conf; \
    touch /var/log/haproxy/traffic /var/log/haproxy/events /var/log/haproxy/errors; \
    chown -R 10001:10001 /etc/haproxy /var/log/haproxy; \
    chmod +x /usr/bin/update-rancher-ssl /usr/bin/loglevel; \
    ln -sf /proc/1/fd/1 /var/log/haproxy/events; \
    ln -sf /proc/1/fd/1 /var/log/haproxy/traffic; \
    ln -sf /proc/1/fd/2 /var/log/haproxy/errors

VOLUME /var/log/haproxy

ENTRYPOINT ["/sbin/tini", "-s", "--"]

CMD ["lb-controller.sh", "--controller", "rancher", "--provider", "haproxy"]

USER 10001:10001
