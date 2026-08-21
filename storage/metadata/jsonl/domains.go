package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ratrektlabs/rakit/storage/metadata"
)

func (s *Store) domainPath(name string) string {
	return filepath.Join(s.storeDir, name+".jsonl")
}

func readLatest(path string) (map[string]json.RawMessage, error) {
	records, err := readEnvelopes(path)
	if err != nil {
		return nil, err
	}
	live := make(map[string]json.RawMessage)
	for _, record := range records {
		switch record.Type {
		case "put":
			if len(record.Payload) == 0 {
				return nil, fmt.Errorf("jsonl: mutable put record %s has no payload in %s", record.EventID, path)
			}
			live[record.Key] = append(json.RawMessage(nil), record.Payload...)
		case "delete":
			delete(live, record.Key)
		default:
			return nil, fmt.Errorf("jsonl: unknown mutable event type %q in %s", record.Type, path)
		}
	}
	return live, nil
}

func (s *Store) appendDomainLocked(path, key string, value any) error {
	e, err := newEnvelope("put", "", key, value)
	if err != nil {
		return err
	}
	if err := appendEnvelope(path, e); err != nil {
		return err
	}
	return s.maybeCompactDomainLocked(path)
}

func (s *Store) deleteDomainLocked(path, key string) error {
	e, err := newEnvelope("delete", "", key, map[string]string{"key": key})
	if err != nil {
		return err
	}
	if err := appendEnvelope(path, e); err != nil {
		return err
	}
	return s.maybeCompactDomainLocked(path)
}

func (s *Store) maybeCompactDomainLocked(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() <= mutableCompactionBytes {
		return nil
	}
	records, err := readEnvelopes(path)
	if err != nil {
		return err
	}
	live := make(map[string]envelope)
	for _, record := range records {
		switch record.Type {
		case "put":
			if len(record.Payload) == 0 {
				return fmt.Errorf("jsonl: mutable put record %s has no payload in %s", record.EventID, path)
			}
			live[record.Key] = record
		case "delete":
			delete(live, record.Key)
		default:
			return fmt.Errorf("jsonl: unknown mutable event type %q in %s", record.Type, path)
		}
	}
	superseded := len(records) - len(live)
	if superseded*2 < len(records) {
		return nil
	}
	keys := make([]string, 0, len(live))
	for key := range live {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	compacted := make([]envelope, 0, len(keys))
	for _, key := range keys {
		compacted = append(compacted, live[key])
	}
	return rewriteEnvelopes(path, compacted)
}

// SaveTool creates or updates a persisted tool definition. Name is the
// logical key, matching the SQLite adapter's global uniqueness semantics.
func (s *Store) SaveTool(ctx context.Context, td *metadata.ToolDef) error {
	if td == nil {
		return errors.New("jsonl: cannot save a nil tool")
	}
	return s.withLock(ctx, func() error {
		path := s.domainPath("tools")
		live, err := readLatest(path)
		if err != nil {
			return fmt.Errorf("jsonl: read tools: %w", err)
		}
		if raw := live[td.Name]; raw != nil {
			var old metadata.ToolDef
			if err := json.Unmarshal(raw, &old); err != nil {
				return fmt.Errorf("jsonl: decode tool %q: %w", td.Name, err)
			}
			td.ID = old.ID
			td.CreatedAt = old.CreatedAt
		} else {
			if td.ID == "" {
				td.ID = newID("tool-")
			}
			if td.CreatedAt == 0 {
				td.CreatedAt = time.Now().UnixMilli()
			}
		}
		if err := s.appendDomainLocked(path, td.Name, td); err != nil {
			return fmt.Errorf("jsonl: save tool %q: %w", td.Name, err)
		}
		return nil
	})
}

// GetTool returns a tool definition by name, or nil when it is absent.
func (s *Store) GetTool(ctx context.Context, name string) (*metadata.ToolDef, error) {
	var result *metadata.ToolDef
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("tools"))
		if err != nil {
			return fmt.Errorf("jsonl: read tools: %w", err)
		}
		raw := live[name]
		if raw == nil {
			return nil
		}
		var td metadata.ToolDef
		if err := json.Unmarshal(raw, &td); err != nil {
			return fmt.Errorf("jsonl: decode tool %q: %w", name, err)
		}
		if td.Headers == nil {
			td.Headers = map[string]string{}
		}
		if td.InputMapping == nil {
			td.InputMapping = map[string]string{}
		}
		result = &td
		return nil
	})
	return result, err
}

// ListTools lists tools belonging to an agent.
func (s *Store) ListTools(ctx context.Context, agentID string) ([]*metadata.ToolDef, error) {
	result := []*metadata.ToolDef{}
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("tools"))
		if err != nil {
			return fmt.Errorf("jsonl: read tools: %w", err)
		}
		for _, raw := range live {
			var td metadata.ToolDef
			if err := json.Unmarshal(raw, &td); err != nil {
				return fmt.Errorf("jsonl: decode tool: %w", err)
			}
			if td.AgentID != agentID {
				continue
			}
			if td.Headers == nil {
				td.Headers = map[string]string{}
			}
			if td.InputMapping == nil {
				td.InputMapping = map[string]string{}
			}
			copy := td
			result = append(result, &copy)
		}
		return nil
	})
	return result, err
}

// DeleteTool removes a tool definition. Deleting a missing tool is a no-op.
func (s *Store) DeleteTool(ctx context.Context, name string) error {
	return s.deleteDomain(ctx, "tools", name)
}

// SaveSkill creates or updates a skill definition.
func (s *Store) SaveSkill(ctx context.Context, def *metadata.SkillDef) error {
	if def == nil {
		return errors.New("jsonl: cannot save a nil skill")
	}
	return s.withLock(ctx, func() error {
		if err := s.appendDomainLocked(s.domainPath("skills"), def.Name, def); err != nil {
			return fmt.Errorf("jsonl: save skill %q: %w", def.Name, err)
		}
		return nil
	})
}

// GetSkill returns a skill definition by name, or nil when absent.
func (s *Store) GetSkill(ctx context.Context, name string) (*metadata.SkillDef, error) {
	var result *metadata.SkillDef
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("skills"))
		if err != nil {
			return fmt.Errorf("jsonl: read skills: %w", err)
		}
		if raw := live[name]; raw != nil {
			var def metadata.SkillDef
			if err := json.Unmarshal(raw, &def); err != nil {
				return fmt.Errorf("jsonl: decode skill %q: %w", name, err)
			}
			result = &def
		}
		return nil
	})
	return result, err
}

// ListSkills lists the lightweight entries for all enabled and disabled skills.
func (s *Store) ListSkills(ctx context.Context) ([]*metadata.SkillEntry, error) {
	result := []*metadata.SkillEntry{}
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("skills"))
		if err != nil {
			return fmt.Errorf("jsonl: read skills: %w", err)
		}
		for _, raw := range live {
			var def metadata.SkillDef
			if err := json.Unmarshal(raw, &def); err != nil {
				return fmt.Errorf("jsonl: decode skill: %w", err)
			}
			result = append(result, &metadata.SkillEntry{
				Name:        def.Name,
				Description: def.Description,
				Version:     def.Version,
				Enabled:     def.Enabled,
			})
		}
		return nil
	})
	return result, err
}

// DeleteSkill removes a skill definition. Deleting a missing skill is a no-op.
func (s *Store) DeleteSkill(ctx context.Context, name string) error {
	return s.deleteDomain(ctx, "skills", name)
}

// SetMemory stores bytes under a scoped memory key.
func (s *Store) SetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string, value []byte) error {
	composite := metadata.ScopedKey(scope, scopeID, key)
	copyValue := make([]byte, len(value))
	copy(copyValue, value)
	return s.withLock(ctx, func() error {
		if err := s.appendDomainLocked(s.domainPath("memory"), composite, memoryValue{Value: copyValue}); err != nil {
			return fmt.Errorf("jsonl: set memory %q: %w", composite, err)
		}
		return nil
	})
}

// GetMemory returns a copy of a scoped memory value, or nil when absent.
func (s *Store) GetMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) ([]byte, error) {
	composite := metadata.ScopedKey(scope, scopeID, key)
	var result []byte
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("memory"))
		if err != nil {
			return fmt.Errorf("jsonl: read memory: %w", err)
		}
		raw := live[composite]
		if raw == nil {
			return nil
		}
		var value memoryValue
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("jsonl: decode memory %q: %w", composite, err)
		}
		result = make([]byte, len(value.Value))
		copy(result, value.Value)
		return nil
	})
	return result, err
}

// DeleteMemory removes a scoped memory value. Missing values are ignored.
func (s *Store) DeleteMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, key string) error {
	return s.deleteDomain(ctx, "memory", metadata.ScopedKey(scope, scopeID, key))
}

// ListMemory returns full composite keys matching the scoped prefix.
func (s *Store) ListMemory(ctx context.Context, scope metadata.MemoryScope, scopeID, prefix string) ([]string, error) {
	compositePrefix := metadata.ScopedKey(scope, scopeID, prefix)
	result := []string{}
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("memory"))
		if err != nil {
			return fmt.Errorf("jsonl: read memory: %w", err)
		}
		for key := range live {
			if strings.HasPrefix(key, compositePrefix) {
				result = append(result, key)
			}
		}
		sort.Strings(result)
		return nil
	})
	return result, err
}

// Set stores a legacy global-scoped key.
func (s *Store) Set(ctx context.Context, key string, value []byte) error {
	return s.SetMemory(ctx, metadata.ScopeGlobal, "", key, value)
}

// Get reads a legacy global-scoped key.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	return s.GetMemory(ctx, metadata.ScopeGlobal, "", key)
}

// Delete removes a legacy global-scoped key.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.DeleteMemory(ctx, metadata.ScopeGlobal, "", key)
}

// List lists legacy global-scoped keys.
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	return s.ListMemory(ctx, metadata.ScopeGlobal, "", prefix)
}

// SaveMCPServer creates or updates an MCP server definition.
func (s *Store) SaveMCPServer(ctx context.Context, server *metadata.MCPServerDef) error {
	if server == nil {
		return errors.New("jsonl: cannot save a nil MCP server")
	}
	return s.withLock(ctx, func() error {
		path := s.domainPath("mcp-servers")
		live, err := readLatest(path)
		if err != nil {
			return fmt.Errorf("jsonl: read MCP servers: %w", err)
		}
		if raw := live[server.Name]; raw != nil {
			var old metadata.MCPServerDef
			if err := json.Unmarshal(raw, &old); err != nil {
				return fmt.Errorf("jsonl: decode MCP server %q: %w", server.Name, err)
			}
			server.ID = old.ID
			server.CreatedAt = old.CreatedAt
		} else {
			if server.ID == "" {
				server.ID = newID("mcp-")
			}
			if server.CreatedAt == 0 {
				server.CreatedAt = time.Now().UnixMilli()
			}
		}
		if server.Transport == "" {
			server.Transport = "http"
		}
		if err := s.appendDomainLocked(path, server.Name, server); err != nil {
			return fmt.Errorf("jsonl: save MCP server %q: %w", server.Name, err)
		}
		return nil
	})
}

// GetMCPServer returns an MCP server by name, or nil when absent.
func (s *Store) GetMCPServer(ctx context.Context, name string) (*metadata.MCPServerDef, error) {
	var result *metadata.MCPServerDef
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("mcp-servers"))
		if err != nil {
			return fmt.Errorf("jsonl: read MCP servers: %w", err)
		}
		if raw := live[name]; raw != nil {
			var server metadata.MCPServerDef
			if err := json.Unmarshal(raw, &server); err != nil {
				return fmt.Errorf("jsonl: decode MCP server %q: %w", name, err)
			}
			if server.Headers == nil {
				server.Headers = map[string]string{}
			}
			if server.Transport == "" {
				server.Transport = "http"
			}
			result = &server
		}
		return nil
	})
	return result, err
}

// ListMCPServers lists MCP servers belonging to an agent.
func (s *Store) ListMCPServers(ctx context.Context, agentID string) ([]*metadata.MCPServerDef, error) {
	result := []*metadata.MCPServerDef{}
	err := s.withLock(ctx, func() error {
		live, err := readLatest(s.domainPath("mcp-servers"))
		if err != nil {
			return fmt.Errorf("jsonl: read MCP servers: %w", err)
		}
		for _, raw := range live {
			var server metadata.MCPServerDef
			if err := json.Unmarshal(raw, &server); err != nil {
				return fmt.Errorf("jsonl: decode MCP server: %w", err)
			}
			if server.AgentID != agentID {
				continue
			}
			if server.Headers == nil {
				server.Headers = map[string]string{}
			}
			if server.Transport == "" {
				server.Transport = "http"
			}
			copy := server
			result = append(result, &copy)
		}
		return nil
	})
	return result, err
}

// DeleteMCPServer removes an MCP server. Deleting a missing server is a no-op.
func (s *Store) DeleteMCPServer(ctx context.Context, name string) error {
	return s.deleteDomain(ctx, "mcp-servers", name)
}

func (s *Store) deleteDomain(ctx context.Context, name, key string) error {
	return s.withLock(ctx, func() error {
		path := s.domainPath(name)
		live, err := readLatest(path)
		if err != nil {
			return fmt.Errorf("jsonl: read %s: %w", name, err)
		}
		if _, ok := live[key]; !ok {
			return nil
		}
		if err := s.deleteDomainLocked(path, key); err != nil {
			return fmt.Errorf("jsonl: delete %s %q: %w", name, key, err)
		}
		return nil
	})
}
