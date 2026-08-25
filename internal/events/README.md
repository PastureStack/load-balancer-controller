# Internal event compatibility code

This package and the sibling `internal/locks` package retain the small Rancher
1.6 event-subscriber protocol surface used for endpoint-drain events. They were
inherited from `rancher/event-subscriber` commit
`60019926e12f2ab1776e0aeccdbb9892046a443d` under Apache-2.0.

PastureStack now owns this code directly so the abandoned module is no longer a
runtime dependency. The protocol fields remain compatible, while URL handling,
logging, response draining, and tests are maintained in this repository.
