package protocol

import (
	"strings"
	"sync"
)

// Registry manages protocol registration and content negotiation.
// It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	protocols map[string]Protocol
	def       Protocol
}

func NewRegistry() *Registry {
	return &Registry{
		protocols: make(map[string]Protocol),
	}
}

func (r *Registry) Register(p Protocol) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.protocols[p.Name()] = p
}

func (r *Registry) Get(name string) Protocol {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.protocols[name]
}

func (r *Registry) SetDefault(p Protocol) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.def = p
}

func (r *Registry) Default() Protocol {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.def
}

// Negotiate selects a protocol based on the Accept header value.
//
//	"text/vnd.ag-ui"    → AG-UI protocol
//	"text/vnd.ai-sdk"   → AI SDK protocol
//	"text/event-stream" → default protocol
//
// Returns nil if no known media type matches.
func (r *Registry) Negotiate(accept string) Protocol {
	r.mu.RLock()
	defer r.mu.RUnlock()

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
