package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"axon/internal/config"
	"axon/internal/orchestr"
	"axon/internal/provider"
	"axon/internal/secret"
)

// envConfig is the CLI's model configuration, read entirely from the
// environment so a colleague can run this binary without a config.json or a
// Keychain entry.
type envConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	Project  string
	DataDir  string
}

func loadEnv() envConfig {
	return envConfig{
		// Provider keeps its case: it doubles as either a protocol name
		// (openai/anthropic, when a key is supplied) or the name of an existing
		// provider in the desktop config, and those names are case-sensitive.
		Provider: strings.TrimSpace(os.Getenv("ORCH_PROVIDER")),
		BaseURL:  strings.TrimSpace(os.Getenv("ORCH_BASE_URL")),
		APIKey:   strings.TrimSpace(os.Getenv("ORCH_API_KEY")),
		Model:    strings.TrimSpace(os.Getenv("ORCH_MODEL")),
		Project:  strings.TrimSpace(os.Getenv("ORCH_PROJECT")),
		DataDir:  strings.TrimSpace(os.Getenv("ORCH_DATA_DIR")),
	}
}

// providerFactory resolves a model client. Environment configuration wins; only
// when ORCH_API_KEY is unset does it fall back to the desktop app's
// config.json + Keychain, which is what makes the same binary work both on the
// author's machine (existing settings) and on a colleague's (three env vars).
func (e envConfig) providerFactory() orchestr.ProviderFactory {
	return func(_ context.Context, providerName, model string) (provider.Provider, string, error) {
		if e.APIKey != "" {
			m := model
			if m == "" {
				m = e.Model
			}
			if m == "" {
				return nil, "", fmt.Errorf("未设置模型：export ORCH_MODEL=<模型id>（如 deepseek-chat）")
			}
			proto := e.Provider
			if proto == "" {
				proto = "openai"
			}
			p, err := buildProvider(proto, e.BaseURL, e.APIKey)
			if err != nil {
				return nil, "", err
			}
			return p, m, nil
		}

		// Fallback: the desktop app's configuration. ORCH_PROVIDER / ORCH_MODEL
		// still act as selectors here (they name an existing provider/model
		// rather than supplying credentials), so "use my deepseek entry" needs no
		// key on the command line.
		cfg, err := config.Load(e.DataDir)
		if err != nil {
			return nil, "", fmt.Errorf("读取 Axon 配置失败: %w", err)
		}
		c := cfg.Get()
		name := providerName
		if name == "" {
			name = e.Provider
		}
		if name == "" {
			name = c.DefaultProvider
		}
		m := model
		if m == "" {
			m = e.Model
		}
		if m == "" {
			m = c.DefaultModel
		}
		if name == "" {
			return nil, "", fmt.Errorf("还没有可用的模型配置\n  最简单的方式: export ORCH_API_KEY=... ORCH_MODEL=... [ORCH_BASE_URL=...]\n  或者在 Axon 桌面版的设置里配置一个 provider")
		}
		pc, ok := cfg.Provider(name)
		if !ok {
			return nil, "", fmt.Errorf("provider %q 未在配置中找到", name)
		}
		key, err := secret.New().Get(pc.KeyRef)
		if err != nil {
			return nil, "", fmt.Errorf("读取 %q 的密钥失败: %w", name, err)
		}
		p, err := buildProvider(pc.Protocol, pc.BaseURL, key)
		if err != nil {
			return nil, "", err
		}
		return p, m, nil
	}
}

// buildProvider instantiates a streaming client for a protocol, filling in the
// vendor's public endpoint when no base URL was given.
func buildProvider(protocol, baseURL, key string) (provider.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "openai":
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return provider.NewOpenAI(baseURL, key), nil
	case "anthropic":
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
		return provider.NewAnthropic(baseURL, key), nil
	default:
		return nil, fmt.Errorf("不支持的协议 %q（可选 openai | anthropic）", protocol)
	}
}
