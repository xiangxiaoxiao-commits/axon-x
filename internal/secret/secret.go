// Package secret stores API keys in the macOS Keychain so they never touch the
// database, config files, logs or exports (security requirement). Callers refer
// to a key by a stable account name; the raw value is resolved only at the
// moment of an API call.
package secret

import "errors"

// keychainService is the Keychain service name grouping all axon secrets.
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
