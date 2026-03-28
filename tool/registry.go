package tool

import "github.com/ratrektlabs/rl-agent/provider"

// Registry manages available tools.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Schema returns tool definitions for provider requests.
func (r *Registry) Schema() []provider.Tool {
	schemas := make([]provider.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, provider.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return schemas
}
