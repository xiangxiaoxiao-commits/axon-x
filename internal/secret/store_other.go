//go:build !darwin && !windows

package secret

import "fmt"

// unsupportedStore is the fallback Store on platforms without a native
// credential vault wired up (currently anything but macOS/Windows). It fails
// loudly rather than silently persisting secrets in plaintext.
type unsupportedStore struct{}

var _ Store = (*unsupportedStore)(nil)

// New returns a Store that reports the platform is unsupported. Axon targets
// macOS and Windows; other platforms compile but cannot store secrets.
func New() Store { return &unsupportedStore{} }

var errUnsupported = fmt.Errorf("secret: no OS credential store on this platform")

func (unsupportedStore) Set(string, string) error   { return errUnsupported }
func (unsupportedStore) Get(string) (string, error) { return "", errUnsupported }
func (unsupportedStore) Delete(string) error        { return errUnsupported }
func (unsupportedStore) Has(string) bool            { return false }
