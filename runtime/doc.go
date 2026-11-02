// Package runtime is the binary's entry point. Both the OSS and paid binaries
// call runtime.Run with their respective ModuleBuilder; everything else —
// config loading, supervisor, server registration — lives here and is
// shared by importing.
//
// This package is the architectural enforcement of "paid layers on OSS,
// no overlap" — paid's main package contributes only a ModuleBuilder
// closure; nothing else.
package runtime
