// Package appinfo exposes static metadata about the Knot backend.
//
// It exists so the foundation has one trivially testable internal package
// without introducing any product-domain behaviour.
package appinfo

// Name is the canonical service name used in logs and diagnostics.
const Name = "knot-backend"

// Version is the current backend version.
//
// It is a placeholder until a real build-time versioning strategy is approved.
const Version = "0.1.0"
