# Compatibility contract

The runtime supports the load-balancer service contract emitted by PastureStack Server:

- HAProxy configuration generated from service metadata;
- HTTP, HTTPS, TCP, certificate, health-check, stickiness, and backend rules supported by the preserved controller;
- control-plane credentials injected by the orchestration engine;
- metadata discovery at the compatibility link-local endpoint;
- graceful container restart and service reconciliation; and
- Ubuntu 26.04 on `linux/amd64`.

Role-specific credentials are preferred. Generic `CATTLE_ACCESS_KEY` and `CATTLE_SECRET_KEY` values are accepted as a compatibility fallback so digest-pinned and older engine paths can start the same release safely.

The packaged rsyslog configuration and daemon options comply with the default Ubuntu 26.04 AppArmor profile so logging initialization cannot prevent HAProxy from starting.

The runtime must be validated through an actual PastureStack Server project with a registered node, a healthy backend service, an HTTP request through the load balancer, and container restart recovery before a release tag is published.

Windows containers and Kubernetes ingress operation are not release-qualified by the PastureStack Server replacement test.
