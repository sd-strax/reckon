// Package stix holds reckon's STIX 2.1 extension definitions and their JSON
// Schemas (adr/0357, design/01 "STIX conformance and change").
//
// Each reckon custom type and each reckon property-extension has one
// extension-definition object. Its id is a fixed UUIDv4, the same in every
// install. Its author is the one publisher identity, Publisher.
//
// Each definition's schema is a file under schemas/<name>/<version>.json,
// embedded in the binary. The definition's schema property is that file's URL
// in the public repository. A published version's file is never edited. A
// change to a definition gets a new version and a new file, and a breaking
// change also gets a new definition id.
package stix
