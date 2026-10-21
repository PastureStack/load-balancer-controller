# PastureStack load-balancer-controller

PastureStack is an independent community effort to preserve, audit, and modernize the Rancher 1.6 ecosystem. It is not affiliated with or endorsed by Rancher Labs or SUSE.

This branch contains the compatibility runtime used by PastureStack Server to operate HAProxy load-balancer services. It is a GitHub fork of [`rancher/lb-controller`](https://github.com/rancher/lb-controller) and preserves the upstream Git history, authorship, dates, tags, Apache License 2.0 text, and copyright notices.

## Runtime image

The maintained runtime release for PastureStack HAProxy load-balancer services is `v0.9.31`:

```text
ghcr.io/pasturestack/load-balancer-service:v0.9.31
```

Pin deployments to its published manifest digest:

```text
ghcr.io/pasturestack/load-balancer-service@sha256:8133f65849afeb1436fc55e476bf3f8d152529af83e2f99151e4ee469ccd9167
```

The image is intended to be managed by PastureStack Server, not started by itself:
the control plane supplies metadata, API credentials, service links, port rules,
and health-check configuration. Selecting this release does not itself upgrade
an existing Server installation or its load-balancer services.

[Release v0.9.31](https://github.com/PastureStack/load-balancer-controller/releases/tag/v0.9.31)
includes the controller archive, source and image SBOMs, checksums, and build
provenance. Protected CI, race tests, source contracts, image/version checks,
source/image vulnerability scans, and attestations passed for the published
artifact. The runtime uses Go 1.27.3 and zlib 1.3.2-r1.

Native PastureStack lifecycle and traffic acceptance, including dependent-service
state transitions, remains pending; the build and supply-chain checks are not a
claim that this platform-level acceptance has passed.

## Compatibility

The controller accepts role-specific credentials when the orchestration engine supplies them:

- `CATTLE_ENVIRONMENT_ADMIN_ACCESS_KEY`
- `CATTLE_ENVIRONMENT_ADMIN_SECRET_KEY`
- `CATTLE_AGENT_ACCESS_KEY`
- `CATTLE_AGENT_SECRET_KEY`

It also accepts the generic `CATTLE_ACCESS_KEY` and `CATTLE_SECRET_KEY` pair used by older compatible control-plane paths. Role-specific values always take precedence. Secret values are never logged.

The HAProxy logging helper keeps its configuration under the standard `/etc/rsyslog.d` path and does not require a daemon PID file. Logging initialization is validated independently before HAProxy configuration is applied.

### Drain weight recovery

The controller records each backend's runtime weight before setting it to zero
for `target.drain`, including drains that already have zero active sessions.
After drain completion or timeout, a healthy endpoint still present in desired
metadata can recover its original weight even when its ID, IP, and generated
configuration have not changed. No reload is forced solely for this recovery.

Only weights changed by this controller are eligible. Existing zero weights,
manual MAINT/DRAIN, configured `disabled` or weight zero, and an operator's later
nonzero weight are preserved. Active drains follow a HAProxy reload; a completed
drain does not adopt an unexplained zero in a different HAProxy process. Recovery
ownership is process-local and is not reconstructed by guessing after a controller
restart.

Pending recovery retries every five seconds and after successful configuration
application. Runtime socket operations have a two-second deadline. A failed or
unverified weight change retains recovery ownership and produces an ERROR with
the endpoint, backend, and intended weight. Successful restoration is logged only
after runtime readback. A metadata target in `stopping` keeps desired weight zero
even when port rules are merged. This lifecycle zero retains recovery ownership
until the same endpoint returns to an eligible running state; an explicit user
weight zero or `disabled` remains protected. Across a HAProxy PID change, an
active drain is reapplied only to an unchanged configured initial weight, never
to a runtime custom weight observed in the replacement process.
If the desired configured weight changes while draining, recovery honors that
new configured weight instead of undoing the user's edit with the old snapshot.

The backend state fields follow the [HAProxy Runtime API management contract](https://docs.haproxy.org/3.4/management.html#show%20servers%20state).

Protocol-critical `CATTLE_*` variables, `io.rancher.*` labels, metadata paths, API types, and provider identifiers remain compatibility interfaces. They do not represent PastureStack branding or affiliation.

## Build and test

The disposable build environment uses Ubuntu 26.04 and a pinned Go toolchain. The shipped runtime uses Alpine 3.24 with only the commands and libraries required by the controller and source-built HAProxy 3.4 LTS:

```sh
make ci
```

The build produces `ghcr.io/pasturestack/load-balancer-service:<version>`.
The obsolete Kubernetes bootstrap sidecar is not part of the maintained product.

The Go runtime now uses Go Modules with four current external modules. The
Rancher 1.6 API, event, and metadata wire contracts required by Server are
maintained as bounded internal compatibility packages instead of abandoned
third-party dependencies.

GitHub workflows run CodeQL plus the same module, race-test, govulncheck,
reproducibility, Trivy, SBOM, and image checks used for releases.

## Security and provenance

See [`ORIGIN.md`](ORIGIN.md) for the preserved upstream boundary and maintenance policy, [`COMPATIBILITY.md`](COMPATIBILITY.md) for the supported runtime contract, and [`SECURITY.md`](SECURITY.md) for reporting guidance.

The root [`LICENSE`](LICENSE) remains authoritative. PastureStack does not claim ownership of upstream work.
