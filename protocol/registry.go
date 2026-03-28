package protocol

import "strings"

// Registry manages protocol registration and content negotiation.
type Registry struct {
	protocols map[string]Protocol
	def       Protocol
}

func NewRegistry() *Registry {
	return &Registry{
		protocols: make(map[string]Protocol),
	}
}

func (r *Registry) Register(p Protocol) {
	r.protocols[p.Name()] = p
}

func (r *Registry) Get(name string) Protocol {
	return r.protocols[name]
}

func (r *Registry) SetDefault(p Protocol) {
	r.def = p
}

func (r *Registry) Default() Protocol {
	return r.def
}

// Negotiate selects a protocol based on the Accept header value.
//
//	"text/vnd.ag-ui"    → AG-UI protocol
//	"text/vnd.ai-sdk"   → AI SDK protocol
//	"text/event-stream" → default protocol
func (r *Registry) Negotiate(accept string) Protocol {
	accept = strings.ToLower(accept)

	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		// Strip quality factor
		if idx := strings.Index(part, ";"); idx != -1 {
			part = part[:idx]
		}

		switch part {
		case "text/vnd.ag-ui", "application/vnd.ag-ui":
			if p := r.protocols["ag-ui"]; p != nil {
				return p
			}
		case "text/vnd.ai-sdk", "application/vnd.ai-sdk":
			if p := r.protocols["ai-sdk"]; p != nil {
				return p
			}
		case "text/event-stream":
			return r.def
		}
	}

	return nil
}
