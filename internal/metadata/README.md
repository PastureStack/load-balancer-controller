# Internal metadata compatibility client

This package owns the small dated-metadata HTTP contract required by the
PastureStack HAProxy service. It replaces the abandoned
`rancher/go-rancher-metadata` dependency while retaining the documented JSON
fields and endpoints used by compatible servers.

The client validates its base URL, keeps redirects on the same origin, bounds
request time and response size, escapes path segments, and has live-contract
and hostile-input tests. Unknown JSON fields remain forward compatible.
