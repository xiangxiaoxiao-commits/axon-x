// Package secret stores API keys in the OS credential store (macOS Keychain,
// Windows Credential Manager) so they never touch the database, config files,
// logs or exports (security requirement). Callers refer to a key by a stable
// account name; the raw value is resolved only at the moment of an API call.
//
// New() returns the platform-appropriate Store. Each backend lives in a
// build-tagged file (keychain.go for darwin, wincred.go for windows) so the
// whole tree cross-compiles: no platform pulls in another platform's cgo/API.
package secret

import "errors"

// keychainService is the credential-store service name grouping all axon
// secrets (Keychain service on macOS, target prefix on Windows).
const keychainService = "com.axon.app"

// ErrNotFound is returned when no secret exists for the given ref.
var ErrNotFound = errors.New("secret not found")

// Store persists and resolves secrets by reference (account name).
type Store interface {
	// Set stores (or replaces) the secret for ref.
	Set(ref, value string) error

	// Get resolves the raw secret for ref, or ErrNotFound if absent.
	Get(ref string) (string, error)

	// Delete removes the secret for ref. Deleting a missing ref is not an error.
	Delete(ref string) error

	// Has reports whether a secret exists for ref, without returning its value.
	Has(ref string) bool
}
