FROM ghcr.io/chen21019/rc16-agent-base:v0.3.2

RUN rm -f /usr/bin/pebble

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
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
