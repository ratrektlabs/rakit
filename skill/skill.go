package skill

// Entry is the lightweight L1 registration record.
type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
}

// Definition is the full L2 skill definition.
type Definition struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Version      string         `json:"version"`
	Instructions string         `json:"instructions"`
	Tools        []ToolDef      `json:"tools"`
	Config       map[string]any `json:"config"`
	Resources    []Resource     `json:"resources"`
}

// ToolDef describes a tool provided by a skill.
type ToolDef struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Parameters    any               `json:"parameters"`
	Handler       string            `json:"handler"` // "http", "script"
	Endpoint      string            `json:"endpoint,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	InputMapping  map[string]string `json:"input_mapping,omitempty"`
	ResponseField string            `json:"response_field,omitempty"` // JSON field to extract from response
	ScriptPath    string            `json:"script_path,omitempty"`
}

// Resource references an L3 asset in blob storage.
type Resource struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "script", "template", "file"
}
