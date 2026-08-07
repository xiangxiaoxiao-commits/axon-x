// Package config persists provider settings as a plain JSON file under the app
// data directory. It never stores API keys — each provider references a keychain
// account (KeyRef); the raw key lives only in the macOS Keychain.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"axon/internal/provider"
)

// fileName is the config file created inside the app data directory.
const fileName = "config.json"

// Config is the persisted application configuration.
type Config struct {
	// Providers are the configured model backends, keyed by their Name.
	Providers []provider.Config `json:"providers"`
	// DefaultProvider is the Name of the provider used when none is specified.
	DefaultProvider string `json:"defaultProvider"`
	// DefaultModel is the model id used when a request omits one.
	DefaultModel string `json:"defaultModel"`
}

// Manager loads and saves Config, guarding concurrent access.
type Manager struct {
	path string
	mu   sync.RWMutex
	cfg  Config
}

// Load reads config.json from dataDir, returning a Manager with an empty
// config if the file does not yet exist (first run).
func Load(dataDir string) (*Manager, error) {
	m := &Manager{path: filepath.Join(dataDir, fileName)}

	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", m.path, err)
	}
	if err := json.Unmarshal(data, &m.cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", m.path, err)
	}
	return m, nil
}

// Get returns a copy of the current config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Copy the slice so callers cannot mutate internal state.
	out := m.cfg
	out.Providers = append([]provider.Config(nil), m.cfg.Providers...)
	return out
}

// Provider returns the config for the named provider, or false if absent.
func (m *Manager) Provider(name string) (provider.Config, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.cfg.Providers {
		if p.Name == name {
			return p, true
		}
	}
	return provider.Config{}, false
}

// UpsertProvider adds or replaces a provider by Name and persists the config.
func (m *Manager) UpsertProvider(p provider.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	replaced := false
	for i := range m.cfg.Providers {
		if m.cfg.Providers[i].Name == p.Name {
			m.cfg.Providers[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		m.cfg.Providers = append(m.cfg.Providers, p)
	}
	if m.cfg.DefaultProvider == "" {
		m.cfg.DefaultProvider = p.Name
	}
	return m.saveLocked()
}

// SetDefaults updates the default provider/model and persists.
func (m *Manager) SetDefaults(providerName, modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.DefaultProvider = providerName
	m.cfg.DefaultModel = modelID
	return m.saveLocked()
}

// saveLocked writes the config atomically (temp file + rename). Caller holds mu.
func (m *Manager) saveLocked() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
