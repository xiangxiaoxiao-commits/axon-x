package secret

import (
	"errors"
	"fmt"

	keychain "github.com/keybase/go-keychain"
)

// KeychainStore implements Store on top of the macOS Keychain. Secrets are
// stored as generic passwords under a single service (keychainService); the
// caller's ref is used as the account name.
type KeychainStore struct{}

// compile-time assertion that KeychainStore satisfies the Store interface.
var _ Store = (*KeychainStore)(nil)

// NewKeychainStore returns a Store backed by the macOS Keychain.
func NewKeychainStore() *KeychainStore {
	return &KeychainStore{}
}

// newItem builds a Keychain item for ref with the store's fixed accessibility
// and synchronization policy (login-unlocked, no iCloud sync).
func newItem(ref string) keychain.Item {
	item := keychain.NewGenericPassword(keychainService, ref, "", nil, "")
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetAccessible(keychain.AccessibleWhenUnlocked)
	item.SetSynchronizable(keychain.SynchronizableNo)
	return item
}

// Set stores (or replaces) the secret for ref.
func (s *KeychainStore) Set(ref, value string) error {
	item := newItem(ref)
	item.SetData([]byte(value))

	err := keychain.AddItem(item)
	if errors.Is(err, keychain.ErrorDuplicateItem) {
		// The item already exists; replace it in place.
		query := newItem(ref)
		update := keychain.NewItem()
		update.SetData([]byte(value))
		if uerr := keychain.UpdateItem(query, update); uerr != nil {
			return fmt.Errorf("update keychain item %q: %w", ref, uerr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("add keychain item %q: %w", ref, err)
	}
	return nil
}

// Get resolves the raw secret for ref, or a wrapped ErrNotFound if absent.
func (s *KeychainStore) Get(ref string) (string, error) {
	query := newItem(ref)
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)

	results, err := keychain.QueryItem(query)
	if err != nil {
		return "", fmt.Errorf("query keychain item %q: %w", ref, err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("keychain item %q: %w", ref, ErrNotFound)
	}
	return string(results[0].Data), nil
}

// Delete removes the secret for ref. Deleting a missing ref is not an error.
func (s *KeychainStore) Delete(ref string) error {
	err := keychain.DeleteGenericPasswordItem(keychainService, ref)
	if err == nil || errors.Is(err, keychain.ErrorItemNotFound) {
		return nil
	}
	return fmt.Errorf("delete keychain item %q: %w", ref, err)
}

// Has reports whether a secret exists for ref, without returning its value.
func (s *KeychainStore) Has(ref string) bool {
	query := newItem(ref)
	query.SetMatchLimit(keychain.MatchLimitOne)
	// SecItemCopyMatching returns nothing unless a return type is requested;
	// ask for attributes only so the secret value is not read into memory.
	query.SetReturnAttributes(true)

	results, err := keychain.QueryItem(query)
	if err != nil {
		return false
	}
	return len(results) > 0
}
