# PastureStack load-balancer-controller

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

This branch contains the compatibility runtime used by PastureStack Server to operate HAProxy load-balancer services. It is a GitHub fork of [`rancher/lb-controller`](https://github.com/rancher/lb-controller) and preserves the upstream Git history, authorship, dates, tags, Apache License 2.0 text, and copyright notices.

## Runtime image

PastureStack Server creates this image automatically when a user adds a load-balancer service:

```text
ghcr.io/pasturestack/load-balancer-service:v0.9.26
```

Release deployments pin the image by digest. The image is not intended to be started by itself because the control plane supplies metadata, API credentials, service links, port rules, and health-check configuration.

## Compatibility

The controller accepts role-specific credentials when the orchestration engine supplies them:

- `CATTLE_ENVIRONMENT_ADMIN_ACCESS_KEY`
- `CATTLE_ENVIRONMENT_ADMIN_SECRET_KEY`
- `CATTLE_AGENT_ACCESS_KEY`
- `CATTLE_AGENT_SECRET_KEY`

It also accepts the generic `CATTLE_ACCESS_KEY` and `CATTLE_SECRET_KEY` pair used by older compatible control-plane paths. Role-specific values always take precedence. Secret values are never logged.

The HAProxy logging helper keeps its configuration under the Ubuntu AppArmor-approved `/etc/rsyslog.d` path and does not create a daemon PID file. A logging-policy denial therefore cannot block HAProxy configuration from being applied.

Protocol-critical `CATTLE_*` variables, `io.rancher.*` labels, metadata paths, API types, and provider identifiers remain compatibility interfaces. They do not represent PastureStack branding or affiliation.

## Build and test

The maintained build uses Ubuntu 26.04, a pinned Go toolchain, and a source-built HAProxy:

```sh
make ci
```

The build produces:

- `ghcr.io/pasturestack/load-balancer-service:<version>`
- `ghcr.io/pasturestack/load-balancer-controller-sidecar:<version>`

No CI/CD workflow is enabled in this repository. Release images are built and verified manually before publication.

## Security and provenance

See [`ORIGIN.md`](ORIGIN.md) for the preserved upstream boundary and maintenance policy, [`COMPATIBILITY.md`](COMPATIBILITY.md) for the supported runtime contract, and [`SECURITY.md`](SECURITY.md) for reporting guidance.

The root [`LICENSE`](LICENSE) remains authoritative. PastureStack does not claim ownership of upstream work.
