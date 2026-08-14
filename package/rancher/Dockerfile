FROM ubuntu:26.04@sha256:678c6550cc43645e08669028bc177f50be4e7c5b8cca677067b1914d4afc7a03

COPY ubuntu-apt.lock /licenses/ubuntu-apt.lock
ADD --checksum=sha256:6077d27c6b6f8b23590cb01ff877ed8c804a67a5442cc32b5a33da10d2bd0e90 https://snapshot.ubuntu.com/ubuntu/20260814T000000Z/pool/main/c/ca-certificates/ca-certificates_20260601~26.04.1_all.deb /tmp/ca-certificates.deb

ARG PASTURESTACK_IMAGE_VERSION=dev
ARG SOURCE_REVISION=unknown
ENV DEBIAN_FRONTEND=noninteractive

LABEL org.opencontainers.image.source="https://github.com/PastureStack/load-balancer-controller" \
      org.opencontainers.image.description="PastureStack load-balancer controller compatibility sidecar image." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${PASTURESTACK_IMAGE_VERSION}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"

RUN set -eux; \
    . /licenses/ubuntu-apt.lock; \
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; \
    rm -rf /tmp/ca-bootstrap; \
    mkdir -p /tmp/ca-bootstrap /etc/ssl/certs; \
    dpkg-deb --extract /tmp/ca-certificates.deb /tmp/ca-bootstrap; \
    find /tmp/ca-bootstrap/usr/share/ca-certificates -type f -name '*.crt' \
        | LC_ALL=C sort \
        | while IFS= read -r certificate; do cat "${certificate}"; printf '\n'; done \
        > /etc/ssl/certs/ca-certificates.crt; \
    test -s /etc/ssl/certs/ca-certificates.crt; \
    test -s /usr/share/keyrings/ubuntu-archive-keyring.gpg; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: resolute resolute-updates resolute-backports resolute-security\nComponents: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\nSnapshot: no\n' \
        "${UBUNTU_APT_SNAPSHOT}" > /etc/apt/sources.list.d/pasturestack-snapshot.sources; \
    printf 'Acquire::Retries "5";\nAcquire::http::Timeout "30";\nAcquire::https::Timeout "30";\nAcquire::http::Pipeline-Depth "0";\nAcquire::https::CaInfo "/etc/ssl/certs/ca-certificates.crt";\nAcquire::https::Verify-Peer "true";\nAcquire::https::Verify-Host "true";\nAcquire::AllowInsecureRepositories "false";\nAPT::Get::AllowUnauthenticated "false";\n' > /etc/apt/apt.conf.d/80pasturestack-retries; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates="${UBUNTU_APT_CA_CERTIFICATES_VERSION}" \
        curl="${UBUNTU_APT_CURL_VERSION}" \
        jq="${UBUNTU_APT_JQ_VERSION}" \
        openssl="${UBUNTU_APT_OPENSSL_VERSION}" \
        unzip="${UBUNTU_APT_UNZIP_VERSION}" \
        wget="${UBUNTU_APT_WGET_VERSION}"; \
    { \
        printf 'snapshot\t%s\n' "${UBUNTU_APT_SNAPSHOT}"; \
        dpkg-query -W -f='${binary:Package}\t${Version}\n' | LC_ALL=C sort; \
    } > /licenses/CONTROLLER-SIDECAR-UBUNTU-APT-PACKAGES.tsv; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/* /tmp/ca-bootstrap /tmp/ca-certificates.deb; \
    rm -f /usr/bin/pebble

COPY update-rancher-ssl /usr/bin/
COPY loglevel /usr/bin/
RUN chmod +x /usr/bin/update-rancher-ssl /usr/bin/loglevel

COPY entry.sh /usr/bin/
COPY lb-controller /usr/bin/
COPY LICENSE ORIGIN.md /usr/share/licenses/load-balancer-controller/
COPY third-party-licenses /usr/share/licenses/load-balancer-controller/third-party/

RUN set -eux; \
    test -s /usr/share/licenses/load-balancer-controller/LICENSE; \
    test -s /usr/share/licenses/load-balancer-controller/ORIGIN.md; \
    test -s /usr/share/licenses/load-balancer-controller/third-party/manifest.tsv

ENTRYPOINT ["/usr/bin/entry.sh"]
