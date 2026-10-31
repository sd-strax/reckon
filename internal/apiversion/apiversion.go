// Package apiversion holds the backend's HTTP API contract version. It is a
// leaf: both sides of the contract import it, so a client checking the version
// (the sidecar) does not link the backend to read one integer (adr/0237).
package apiversion

// Current is the backend's HTTP API contract version — a single integer
// bumped only on a breaking change to the /api surface the workbench consumes
// (route removal, response-shape change, auth-flow change). It is NOT the
// product/marketing version; it is the compatibility signal the workbench pins
// against, the same philosophy as the adapter plugin protocol version
// (11 §4.1). A client that supports version N keeps working against every
// backend that serves N.
const Current = 1
