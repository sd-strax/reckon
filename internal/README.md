# internal/

Truly internal helpers — utilities, shared infrastructure, and engine-private packages that are not part of the OSS-public commitment.

Go's `internal/` import-path convention restricts imports to the parent of `internal/` and its descendants; this is what enforces the "not part of the public API" guarantee.

## Status

Packages land here alongside the code that needs them. Anything code outside this module legitimately depends on graduates out of `internal/` into a public package; the rest stays restricted.
