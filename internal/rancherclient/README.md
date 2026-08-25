# Internal compatible API bindings

This directory contains the generated Rancher 1.6 API and catalog schema types
required by the PastureStack load-balancer service. The source
was inherited from `rancher/go-rancher` at commit
`cbc1b0a3f68db14c8d074ffc7fe2ca3b26a82c91` under Apache-2.0.

It is kept as internal protocol compatibility code rather than an external,
unmaintained dependency. PastureStack maintains the HTTP/WebSocket transport,
tests, dependency versions, and security fixes; generated resource names and
JSON fields remain stable for compatible servers.
