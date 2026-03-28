package skill

import (
	"context"
	"encoding/json"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

// Registry manages L1 skill entries backed by the metadata store.
type Registry struct {
	store metadata.Store
}

func NewRegistry(store metadata.Store) *Registry {
	return &Registry{store: store}
}

// List returns all registered skill entries (L1 only).
func (r *Registry) List(ctx context.Context) ([]*Entry, error) {
	entries, err := r.store.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*Entry, len(entries))
	for i, e := range entries {
		out[i] = &Entry{
			Name:        e.Name,
			Description: e.Description,
			Version:     e.Version,
			Enabled:     e.Enabled,
		}
	}
	return out, nil
}

// Register adds a new skill definition (stores L1 + L2).
func (r *Registry) Register(ctx context.Context, def *Definition) error {
	tools := make([]any, len(def.Tools))
	for i, t := range def.Tools {
		tools[i] = t
	}

	resources := make([]any, len(def.Resources))
	for i, res := range def.Resources {
		resources[i] = res
	}

	return r.store.SaveSkill(ctx, &metadata.SkillDef{
		Name:         def.Name,
		Description:  def.Description,
		Version:      def.Version,
		Instructions: def.Instructions,
		Tools:        tools,
		Config:       def.Config,
		Resources:    resources,
		Enabled:      true,
	})
}

// Unregister removes a skill.
func (r *Registry) Unregister(ctx context.Context, name string) error {
	return r.store.DeleteSkill(ctx, name)
}

// Enable toggles a skill on.
func (r *Registry) Enable(ctx context.Context, name string) error {
	def, err := r.store.GetSkill(ctx, name)
	if err != nil {
		return err
	}
	def.Enabled = true
	return r.store.SaveSkill(ctx, def)
}

// Disable toggles a skill off without removing it.
func (r *Registry) Disable(ctx context.Context, name string) error {
	def, err := r.store.GetSkill(ctx, name)
	if err != nil {
		return err
	}
	def.Enabled = false
	return r.store.SaveSkill(ctx, def)
}

// Get loads the full L2 definition for a skill.
func (r *Registry) Get(ctx context.Context, name string) (*Definition, error) {
	sd, err := r.store.GetSkill(ctx, name)
	if err != nil {
		return nil, err
	}

	var tools []ToolDef
	for _, t := range sd.Tools {
		b, err := json.Marshal(t)
		if err != nil {
			continue
		}
		var td ToolDef
		if err := json.Unmarshal(b, &td); err != nil {
			continue
		}
		tools = append(tools, td)
	}

	var resources []Resource
	for _, res := range sd.Resources {
		b, err := json.Marshal(res)
		if err != nil {
			continue
		}
		var r Resource
		if err := json.Unmarshal(b, &r); err != nil {
			continue
		}
		resources = append(resources, r)
	}

	return &Definition{
		Name:         sd.Name,
		Description:  sd.Description,
		Version:      sd.Version,
		Instructions: sd.Instructions,
		Tools:        tools,
		Config:       sd.Config,
		Resources:    resources,
	}, nil
}
