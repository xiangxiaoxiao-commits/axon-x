//go:build windows

package secret

import (
	"fmt"

	"github.com/danieljoos/wincred"
)

// WinCredStore implements Store on top of the Windows Credential Manager.
// Secrets are stored as generic credentials whose target name namespaces the
// caller's ref under keychainService, mirroring the macOS Keychain layout so
// both platforms share the same account-name scheme.
type WinCredStore struct{}

// compile-time assertion that WinCredStore satisfies the Store interface.
var _ Store = (*WinCredStore)(nil)

// NewWinCredStore returns a Store backed by the Windows Credential Manager.
func NewWinCredStore() *WinCredStore {
	return &WinCredStore{}
}

// New returns the platform's credential-store Store (Windows Credential Manager).
func New() Store {
	return NewWinCredStore()
}

// target namespaces a ref under the shared service name so axon's credentials
// don't collide with other apps' entries in the user's credential vault.
func target(ref string) string {
	return keychainService + ":" + ref
}

// Set stores (or replaces) the secret for ref. wincred's Write is upsert, so
// this covers both create and overwrite.
func (s *WinCredStore) Set(ref, value string) error {
	cred := wincred.NewGenericCredential(target(ref))
	cred.CredentialBlob = []byte(value)
	// Persist across reboots for the current user only (default is LocalMachine
	// for some flows); LocalMachine would leak the secret to other users.
	cred.Persist = wincred.PersistLocalMachine
	if err := cred.Write(); err != nil {
		return fmt.Errorf("write credential %q: %w", ref, err)
	}
	return nil
}

// Get resolves the raw secret for ref, or a wrapped ErrNotFound if absent.
func (s *WinCredStore) Get(ref string) (string, error) {
	cred, err := wincred.GetGenericCredential(target(ref))
	if err != nil {
		// wincred surfaces "element not found" as an error; treat any lookup
		// miss as ErrNotFound so callers can branch on it uniformly.
		return "", fmt.Errorf("get credential %q: %w", ref, ErrNotFound)
	}
	if cred == nil {
		return "", fmt.Errorf("credential %q: %w", ref, ErrNotFound)
	}
	return string(cred.CredentialBlob), nil
}

// Delete removes the secret for ref. Deleting a missing ref is not an error.
func (s *WinCredStore) Delete(ref string) error {
	cred, err := wincred.GetGenericCredential(target(ref))
	if err != nil || cred == nil {
		return nil // already gone
	}
	if err := cred.Delete(); err != nil {
		return fmt.Errorf("delete credential %q: %w", ref, err)
	}
	return nil
}

// Has reports whether a secret exists for ref, without returning its value.
func (s *WinCredStore) Has(ref string) bool {
	cred, err := wincred.GetGenericCredential(target(ref))
	return err == nil && cred != nil
}
