# Compatibility contract

The runtime supports the load-balancer service contract emitted by PastureStack Server:

- HAProxy configuration generated from service metadata;
- HTTP, HTTPS, TCP, certificate, health-check, stickiness, and backend rules supported by the preserved controller;
- control-plane credentials injected by the orchestration engine;
- metadata discovery at the compatibility link-local endpoint;
- graceful container restart and service reconciliation; and
- Alpine 3.24 on `linux/amd64`.

Role-specific credentials are preferred. Generic `CATTLE_ACCESS_KEY` and `CATTLE_SECRET_KEY` values are accepted as a compatibility fallback so digest-pinned and older engine paths can start the same release safely.

The packaged rsyslog configuration is validated in the Alpine runtime, uses an unprivileged local UDP listener, and cannot prevent HAProxy configuration validation from running.

The runtime must be validated through an actual PastureStack Server project with a registered node, a healthy backend service, an HTTP request through the load balancer, and container restart recovery before a release tag is published.

Windows containers are not release-qualified by the PastureStack Server replacement test.

The maintained binary intentionally exposes only the shipped `rancher` controller and
`haproxy` provider. The unqualified Kubernetes 1.4 ingress, cross-region GLB, and
control-plane LB-provider plugins were removed: none were used by the packaged command,
and retaining them pulled unsupported 2016 Kubernetes libraries into the runtime. Their
obsolete Kubernetes manifests are intentionally absent as well, so the repository does
not advertise an unsupported deployment path or retain example credentials.
