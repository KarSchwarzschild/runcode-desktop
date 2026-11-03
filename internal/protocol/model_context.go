package protocol

// ModelContextValues are application budgets, not claims about provider capacity.
// Zero disables context/history control; zero output leaves the provider default.
type ModelContextValues struct {
	MaxOutputTokens    int `json:"maxOutputTokens"`
	MaxContextTokens   int `json:"maxContextTokens"`
	MaxHistoryMessages int `json:"maxHistoryMessages"`
}

// ModelContextOverride uses nil to inherit. Explicit zero survives JSON and storage.
type ModelContextOverride struct {
	MaxOutputTokens    *int `json:"maxOutputTokens,omitempty"`
	MaxContextTokens   *int `json:"maxContextTokens,omitempty"`
	MaxHistoryMessages *int `json:"maxHistoryMessages,omitempty"`
}

// ModelContextPreference stores one credential-free model override.
type ModelContextPreference struct {
	Model     ModelReference       `json:"model"`
	Overrides ModelContextOverride `json:"overrides"`
}

// ModelContextView separates declared defaults, local overrides and resolved values.
type ModelContextView struct {
	Model     ModelReference       `json:"model"`
	Defaults  *ModelContextValues  `json:"defaults,omitempty"`
	Overrides ModelContextOverride `json:"overrides"`
	Effective ModelContextValues   `json:"effective"`
	Warning   string               `json:"warning,omitempty"`
}

// SaveModelContextSettingsRequest replaces or clears one model preference.
type SaveModelContextSettingsRequest struct {
	Model     ModelReference       `json:"model"`
	Overrides ModelContextOverride `json:"overrides"`
}
