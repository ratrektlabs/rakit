package skill

import (
	"context"

	"github.com/ratrektlabs/rl-agent/tool"
)

// Loader fetches L2 definitions from the metadata store and converts them
// into executable tools.
type Loader struct {
	reg *Registry
}

func NewLoader(reg *Registry) *Loader {
	return &Loader{reg: reg}
}

// LoadEnabled fetches definitions for all enabled skills and returns
// their tools.
func (l *Loader) LoadEnabled(ctx context.Context, resMgr *ResourceManager) ([]tool.Tool, error) {
	entries, err := l.reg.List(ctx)
	if err != nil {
		return nil, err
	}

	var tools []tool.Tool
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		def, err := l.reg.Get(ctx, entry.Name)
		if err != nil {
			continue
		}

		skillTools, err := ToTools(def, resMgr)
		if err != nil {
			continue
		}
		tools = append(tools, skillTools...)
	}
	return tools, nil
}

// ToTools converts a Definition's ToolDefs into executable tool.Tools.
func ToTools(def *Definition, resMgr *ResourceManager) ([]tool.Tool, error) {
	var tools []tool.Tool
	for _, td := range def.Tools {
		switch td.Handler {
		case "http":
			tools = append(tools, &HTTPTool{
				name:        td.Name,
				description: td.Description,
				parameters:  td.Parameters,
				endpoint:    td.Endpoint,
				headers:     td.Headers,
				inputMap:    td.InputMapping,
			})
		case "script":
			tools = append(tools, &ScriptTool{
				name:        td.Name,
				description: td.Description,
				parameters:  td.Parameters,
				scriptPath:  td.ScriptPath,
				resources:   resMgr,
			})
		}
	}
	return tools, nil
}
