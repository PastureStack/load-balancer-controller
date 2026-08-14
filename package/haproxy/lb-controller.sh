#!/bin/bash
set -euo pipefail

METADATA_ADDRESS=${RANCHER_METADATA_ADDRESS:-169.254.169.250}

# to solve this issue https://github.com/rancher/rancher/issues/10074
while ! curl -s -f "http://${METADATA_ADDRESS}/2015-12-19/self/service/uuid"; do
    echo Waiting for metadata self service
    sleep 1
done

ssl_cert_file=$(/usr/bin/update-rancher-ssl)
if [ -n "$ssl_cert_file" ]; then
    export SSL_CERT_FILE="$ssl_cert_file"
fi

if [ "$(id -u)" = "0" ]; then
    exec setpriv --reuid=10001 --regid=10001 --init-groups lb-controller "$@"
fi

exec lb-controller "$@"
