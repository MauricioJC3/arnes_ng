package app

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// ProviderKeyEnv maps a provider name to the env var holding its API key.
var ProviderKeyEnv = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"deepseek":  "DEEPSEEK_API_KEY",
	"kimi":      "MOONSHOT_API_KEY",
	"openai":    "OPENAI_API_KEY",
}

// Connect implements command.Connector: switch provider (and optionally model /
// api key), rebuild the agent, and persist the choice to the config file.
func (a *App) Connect(providerName, model, apiKey string) (string, error) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if _, ok := ProviderKeyEnv[providerName]; !ok {
		return "", fmt.Errorf("provider desconocido: %q (anthropic|deepseek|kimi|openai)", providerName)
	}

	next := a.cfg.Clone()
	next.Provider = providerName
	if model != "" {
		next.Model = model
	}
	if apiKey != "" {
		next.SetKey(providerName, apiKey)
	}

	p, name, err := ProviderFromConfig(MergeEnvKeys(next))
	if err != nil {
		return "", err
	}

	a.cfg = next
	a.prov = p
	a.providerName = name
	a.rebuild(a.sess, a.sess.Messages)

	if err := a.cfg.Save(a.cfgPath); err != nil {
		return "", fmt.Errorf("conectado, pero no se pudo guardar en %s: %w", a.cfgPath, err)
	}

	extra := ""
	if apiKey != "" {
		extra = " · api key guardada"
	}
	return fmt.Sprintf("conectado: %s · modelo %s%s\nconfig: %s", name, p.Model(), extra, a.cfgPath), nil
}

// ActiveProvider implements command.Modeler.
func (a *App) ActiveProvider() string { return a.providerName }

// Model implements command.Modeler.
func (a *App) Model() string { return a.prov.Model() }

// KeyedProviders implements command.Modeler: the active provider first, then any
// other provider that has an API key configured (file or environment).
func (a *App) KeyedProviders() []string {
	merged := MergeEnvKeys(a.cfg)
	var rest []string
	for name := range ProviderKeyEnv {
		if name == a.providerName {
			continue
		}
		if strings.TrimSpace(merged.Keys[name]) != "" {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append([]string{a.providerName}, rest...)
}

// SetModel implements command.Modeler: change the model on the active provider
// and persist it to the config file.
func (a *App) SetModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("modelo vacío")
	}
	a.prov.SetModel(model)
	a.cfg.Model = model
	if err := a.cfg.Save(a.cfgPath); err != nil {
		return "", fmt.Errorf("modelo cambiado a %s, pero no se pudo guardar en %s: %w", a.prov.Model(), a.cfgPath, err)
	}
	return fmt.Sprintf("modelo: %s (%s)", a.prov.Model(), a.providerName), nil
}

// MergeEnvKeys returns a copy of cfg with any API key present in the environment
// layered on top. Provider/model are NOT touched here.
func MergeEnvKeys(cfg config.Config) config.Config {
	out := cfg.Clone()
	for prov, env := range ProviderKeyEnv {
		if v := os.Getenv(env); v != "" {
			out.SetKey(prov, v)
		}
	}
	return out
}

// ProviderFromConfig builds the provider named by cfg.Provider (default
// anthropic), using cfg.Model and cfg.Keys. Model defaults are placeholders --
// change them with /model or /connect.
func ProviderFromConfig(cfg config.Config) (provider.Provider, string, error) {
	name := strings.ToLower(cmp.Or(cfg.Provider, "anthropic"))
	model := cfg.Model
	key := func(p string) string { return cfg.Keys[p] }

	switch name {
	case "anthropic":
		var opts []option.RequestOption
		if k := key("anthropic"); k != "" {
			opts = append(opts, option.WithAPIKey(k))
		}
		p := provider.NewAnthropic(opts...)
		if model != "" {
			p.SetModel(model)
		}
		return p, name, nil
	case "deepseek":
		return provider.NewDeepSeek(key("deepseek"), cmp.Or(model, "deepseek-v4-flash")), name, nil
	case "kimi":
		return provider.NewKimi(key("kimi"), cmp.Or(model, "moonshot-v1-8k")), name, nil
	case "openai":
		return provider.NewOpenAI(key("openai"), cmp.Or(model, "gpt-4o")), name, nil
	default:
		return nil, "", fmt.Errorf("provider desconocido: %q (anthropic|deepseek|kimi|openai)", name)
	}
}

// ListModels builds a throwaway provider for providerName -- using apiKey, or
// the key already known in base when apiKey is empty -- and asks it for the
// model ids it can serve. Used by the /connect picker.
func ListModels(ctx context.Context, base config.Config, providerName, apiKey string) ([]string, error) {
	keys := map[string]string{}
	for k, v := range base.Keys {
		keys[k] = v
	}
	if apiKey != "" {
		keys[providerName] = apiKey
	}
	p, _, err := ProviderFromConfig(config.Config{Provider: providerName, Keys: keys})
	if err != nil {
		return nil, err
	}
	lister, ok := p.(provider.ModelLister)
	if !ok {
		return nil, fmt.Errorf("%s no permite listar modelos", providerName)
	}
	return lister.ListModels(ctx)
}
