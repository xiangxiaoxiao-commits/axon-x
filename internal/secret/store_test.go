//go:build darwin || windows

package secret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

// randomRef returns a unique, namespaced account name so the test never
// collides with or clobbers real secrets in the OS credential store.
func randomRef(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return "axon-test-" + hex.EncodeToString(b)
}

func TestStore_RoundTrip(t *testing.T) {
	store := New()
	ref := randomRef(t)
	const value = "sk-test-secret-value"

	// Best-effort cleanup even if the test fails midway.
	t.Cleanup(func() {
		_ = store.Delete(ref)
	})

	if err := store.Set(ref, value); err != nil {
		// A headless/CI machine may have no usable Keychain; skip rather
		// than fail so the suite stays green off a developer's box.
		t.Skipf("keychain unavailable: %v", err)
	}

	if !store.Has(ref) {
		t.Fatalf("Has(%q) = false after Set, want true", ref)
	}

	got, err := store.Get(ref)
	if err != nil {
		t.Fatalf("Get(%q): %v", ref, err)
	}
	if got != value {
		t.Fatalf("Get(%q) = %q, want %q", ref, got, value)
	}

	if err := store.Delete(ref); err != nil {
		t.Fatalf("Delete(%q): %v", ref, err)
	}

	if store.Has(ref) {
		t.Fatalf("Has(%q) = true after Delete, want false", ref)
	}

	if _, err := store.Get(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(%q) after Delete: got err %v, want ErrNotFound", ref, err)
	}
}

func TestStore_SetOverwrites(t *testing.T) {
	store := New()
	ref := randomRef(t)
	t.Cleanup(func() {
		_ = store.Delete(ref)
	})

	if err := store.Set(ref, "first"); err != nil {
		t.Skipf("keychain unavailable: %v", err)
	}
	if err := store.Set(ref, "second"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}

	got, err := store.Get(ref)
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if got != "second" {
		t.Fatalf("Get after overwrite = %q, want %q", got, "second")
	}
}

func TestStore_DeleteMissingIsNoError(t *testing.T) {
	store := New()
	if err := store.Delete(randomRef(t)); err != nil {
		t.Fatalf("Delete of missing ref: got %v, want nil", err)
	}
}
