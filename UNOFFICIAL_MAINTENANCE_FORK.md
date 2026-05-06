# Unofficial Rancher 1.6 Maintenance Fork

This repository is maintained as an unofficial Rancher 1.6 compatibility and security maintenance fork.

It is not produced, endorsed, sponsored, certified, or supported by Rancher Labs, SUSE, or the upstream Rancher project. Upstream copyright notices and protocol-compatible Rancher names are retained only for license attribution and Rancher 1.6 compatibility.

## Compatibility Scope

This fork preserves Rancher 1.6 load-balancer behavior while allowing maintenance changes for modern operating systems, Docker versions, build tooling, and security requirements.

Do not rename or change protocol-critical Rancher 1.6 identifiers unless a compatibility audit proves the change is safe. This includes API paths, event names, metadata paths, Docker labels, database schema names, catalog schema fields, HAProxy template semantics, and system service behavior.
