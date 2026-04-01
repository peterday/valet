package provider

// Provider describes an API key provider — where to sign up, what keys
// it needs, how to validate them, and rotation characteristics.
type Provider struct {
	Name        string   // canonical name (e.g. "openai")
	DisplayName string   // human-friendly (e.g. "OpenAI")
	SetupURL    string   // shortest path to getting a key
	EnvVars     []EnvVar // keys this provider declares
	FreeTier    string   // short description ("1000 req/month" or "")
	Validate    *ValidationEndpoint
	Rotation    RotationInfo
	MasterKey   *MasterKeyInfo // nil if not supported
}

// EnvVar is a single environment variable declared by a provider.
type EnvVar struct {
	Name      string // e.g. "OPENAI_API_KEY"
	Prefix    string // e.g. "sk-proj-" — for format validation
	Sensitive bool   // if true, extra warnings (e.g. Supabase service_role)
}

// ValidationEndpoint describes how to test if a key works.
type ValidationEndpoint struct {
	Method    string // "GET" or "POST"
	URL       string // endpoint URL (may contain {VAR} placeholders)
	AuthStyle string // "bearer", "header:x-api-key", "basic"
	CostsCredits bool // true if validation consumes credits (e.g. Anthropic)
}

// RotationInfo describes how key rotation works for this provider.
type RotationInfo struct {
	Strategy     string // "create-then-revoke", "rolling", "nuclear", "manual"
	Programmatic bool   // can be done via API
	Warning      string // e.g. "Invalidates ALL existing keys immediately"
}

// MasterKeyInfo describes programmatic key creation from a master key.
type MasterKeyInfo struct {
	EnvVar string // the master key env var (e.g. "OPENAI_ADMIN_KEY")
	Prefix string // expected prefix (e.g. "sk-admin-")
	Note   string // how it works
}

// registry is the built-in provider database.
var registry = map[string]*Provider{
	"openai": {
		Name:        "openai",
		DisplayName: "OpenAI",
		SetupURL:    "https://platform.openai.com/api-keys",
		EnvVars: []EnvVar{
			{Name: "OPENAI_API_KEY", Prefix: "sk-proj-"},
		},
		FreeTier: "",
		Validate: &ValidationEndpoint{
			Method:    "GET",
			URL:       "https://api.openai.com/v1/models",
			AuthStyle: "bearer",
		},
		Rotation: RotationInfo{
			Strategy:     "create-then-revoke",
			Programmatic: true,
		},
		MasterKey: &MasterKeyInfo{
			EnvVar: "OPENAI_ADMIN_KEY",
			Prefix: "sk-admin-",
			Note:   "Org Owner admin key; creates project-scoped keys via service accounts",
		},
	},
	"anthropic": {
		Name:        "anthropic",
		DisplayName: "Anthropic",
		SetupURL:    "https://console.anthropic.com/settings/keys",
		EnvVars: []EnvVar{
			{Name: "ANTHROPIC_API_KEY", Prefix: "sk-ant-api03-"},
		},
		FreeTier: "$5 free credits",
		Validate: &ValidationEndpoint{
			Method:       "POST",
			URL:          "https://api.anthropic.com/v1/messages",
			AuthStyle:    "header:x-api-key",
			CostsCredits: true,
		},
		Rotation: RotationInfo{
			Strategy:     "create-then-revoke",
			Programmatic: false,
		},
	},
	"stripe": {
		Name:        "stripe",
		DisplayName: "Stripe",
		SetupURL:    "https://dashboard.stripe.com/test/apikeys",
		EnvVars: []EnvVar{
			{Name: "STRIPE_SECRET_KEY", Prefix: "sk_test_"},
			{Name: "STRIPE_PUBLISHABLE_KEY", Prefix: "pk_test_"},
			{Name: "STRIPE_WEBHOOK_SECRET", Prefix: "whsec_"},
		},
		FreeTier: "Test mode is free, unlimited",
		Validate: &ValidationEndpoint{
			Method:    "GET",
			URL:       "https://api.stripe.com/v1/balance",
			AuthStyle: "basic",
		},
		Rotation: RotationInfo{
			Strategy:     "rolling",
			Programmatic: false,
			Warning:      "Use Stripe's rolling key feature — old key stays valid during grace period (default 24h)",
		},
	},
	"supabase": {
		Name:        "supabase",
		DisplayName: "Supabase",
		SetupURL:    "https://supabase.com/dashboard/project/_/settings/api",
		EnvVars: []EnvVar{
			{Name: "SUPABASE_URL"},
			{Name: "SUPABASE_ANON_KEY", Prefix: "eyJhbGci"},
			{Name: "SUPABASE_SERVICE_ROLE_KEY", Prefix: "eyJhbGci", Sensitive: true},
		},
		FreeTier: "2 free projects, no card required",
		Validate: &ValidationEndpoint{
			Method:    "GET",
			URL:       "{SUPABASE_URL}/rest/v1/",
			AuthStyle: "header:apikey",
		},
		Rotation: RotationInfo{
			Strategy:     "nuclear",
			Programmatic: false,
			Warning:      "Rotating the JWT secret invalidates ALL existing keys immediately",
		},
	},
	"fly": {
		Name:        "fly",
		DisplayName: "Fly.io",
		SetupURL:    "https://fly.io/user/personal_access_tokens",
		EnvVars: []EnvVar{
			{Name: "FLY_API_TOKEN", Prefix: "fo1_"},
		},
		FreeTier: "3 free VMs",
		Validate: &ValidationEndpoint{
			Method:    "GET",
			URL:       "https://api.machines.dev/v1/apps",
			AuthStyle: "bearer",
		},
		Rotation: RotationInfo{
			Strategy:     "create-then-revoke",
			Programmatic: true,
		},
		MasterKey: &MasterKeyInfo{
			EnvVar: "FLY_API_TOKEN",
			Prefix: "fo1_",
			Note:   "Use 'flyctl tokens create' to generate scoped deploy tokens",
		},
	},
	"exa": {
		Name:        "exa",
		DisplayName: "Exa",
		SetupURL:    "https://dashboard.exa.ai/api-keys",
		EnvVars: []EnvVar{
			{Name: "EXA_API_KEY"},
		},
		FreeTier: "1000 requests/month",
		Validate: &ValidationEndpoint{
			Method:    "POST",
			URL:       "https://api.exa.ai/search",
			AuthStyle: "header:x-api-key",
		},
		Rotation: RotationInfo{
			Strategy:     "manual",
			Programmatic: false,
		},
	},
	"elevenlabs": {
		Name:        "elevenlabs",
		DisplayName: "ElevenLabs",
		SetupURL:    "https://elevenlabs.io/app/settings/api-keys",
		EnvVars: []EnvVar{
			{Name: "ELEVEN_LABS_API_KEY", Prefix: "sk_"},
		},
		FreeTier: "10,000 characters/month",
		Validate: &ValidationEndpoint{
			Method:    "GET",
			URL:       "https://api.elevenlabs.io/v1/user",
			AuthStyle: "header:xi-api-key",
		},
		Rotation: RotationInfo{
			Strategy:     "manual",
			Programmatic: false,
		},
	},
}

// Get returns a provider by canonical name, or nil if not found.
func Get(name string) *Provider {
	return registry[name]
}

// FindByEnvVar searches the registry for a provider that declares the
// given environment variable name. Returns nil if no match.
func FindByEnvVar(envVar string) *Provider {
	for _, p := range registry {
		for _, ev := range p.EnvVars {
			if ev.Name == envVar {
				return p
			}
		}
	}
	return nil
}

// All returns all registered providers.
func All() map[string]*Provider {
	return registry
}

// CheckPrefix validates that a value matches the expected prefix for an
// env var. Returns the provider name and expected prefix if there's a
// mismatch, or empty strings if ok (or no prefix defined).
func CheckPrefix(envVar, value string) (providerName, expectedPrefix string, ok bool) {
	p := FindByEnvVar(envVar)
	if p == nil {
		return "", "", true // no provider, no check
	}
	for _, ev := range p.EnvVars {
		if ev.Name == envVar && ev.Prefix != "" {
			if len(value) >= len(ev.Prefix) && value[:len(ev.Prefix)] == ev.Prefix {
				return p.DisplayName, ev.Prefix, true
			}
			return p.DisplayName, ev.Prefix, false
		}
	}
	return "", "", true
}
