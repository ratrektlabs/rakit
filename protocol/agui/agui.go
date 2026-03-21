package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ratrektlabs/rl-agent/agent"
	"github.com/ratrektlabs/rl-agent/provider"
)

type EventType string

const (
	EventRunStarted         EventType = "RUN_STARTED"
	EventRunFinished        EventType = "RUN_FINISHED"
	EventRunError           EventType = "RUN_ERROR"
	EventStepStarted        EventType = "STEP_STARTED"
	EventStepFinished       EventType = "STEP_FINISHED"
	EventTextMessageStart   EventType = "TEXT_MESSAGE_START"
	EventTextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd     EventType = "TEXT_MESSAGE_END"
	EventToolCallStart      EventType = "TOOL_CALL_START"
	EventToolCallArgs       EventType = "TOOL_CALL_ARGS"
	EventToolCallEnd        EventType = "TOOL_CALL_END"
	EventStateSnapshot      EventType = "STATE_SNAPSHOT"
	EventStateDelta         EventType = "STATE_DELTA"
	EventMessagesSnapshot   EventType = "MESSAGES_SNAPSHOT"
	EventRaw                EventType = "RAW"
	EventCustom             EventType = "CUSTOM"
)

type BaseEvent struct {
	Type      EventType `json:"type"`
	Timestamp int64     `json:"timestamp"`
	RunID     string    `json:"run_id,omitempty"`
	ThreadID  string    `json:"thread_id,omitempty"`
}

type RunStartedEvent struct {
	BaseEvent
	Model     string `json:"model"`
	ThreadID  string `json:"thread_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type RunFinishedEvent struct {
	BaseEvent
	Steps       int            `json:"steps"`
	Usage       provider.Usage `json:"usage"`
	FinalOutput string         `json:"final_output,omitempty"`
}

type RunErrorEvent struct {
	BaseEvent
	Error  string `json:"error"`
	Code   string `json:"code,omitempty"`
	Step   int    `json:"step,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type StepStartedEvent struct {
	BaseEvent
	Step int `json:"step"`
}

type StepFinishedEvent struct {
	BaseEvent
	Step       int    `json:"step"`
	ToolCalls  int    `json:"tool_calls,omitempty"`
	HasContent bool   `json:"has_content,omitempty"`
	Status     string `json:"status"`
}

type TextMessageStartEvent struct {
	BaseEvent
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
}

type TextMessageContentEvent struct {
	BaseEvent
	MessageID string `json:"message_id"`
	Delta     string `json:"delta"`
}

type TextMessageEndEvent struct {
	BaseEvent
	MessageID string `json:"message_id"`
}

type ToolCallStartEvent struct {
	BaseEvent
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

type ToolCallArgsEvent struct {
	BaseEvent
	ToolCallID string `json:"tool_call_id"`
	Delta      string `json:"delta"`
}

type ToolCallEndEvent struct {
	BaseEvent
	ToolCallID string `json:"tool_call_id"`
	Result     string `json:"result,omitempty"`
	Success    bool   `json:"success"`
}

type StateSnapshotEvent struct {
	BaseEvent
	State map[string]interface{} `json:"state"`
}

type StateDeltaEvent struct {
	BaseEvent
	Delta []StateDelta `json:"delta"`
}

type StateDelta struct {
	Path  string      `json:"path"`
	Op    string      `json:"op"`
	Value interface{} `json:"value,omitempty"`
}

type MessagesSnapshotEvent struct {
	BaseEvent
	Messages []provider.Message `json:"messages"`
}

type RawEvent struct {
	BaseEvent
	Event interface{} `json:"event"`
}

type CustomEvent struct {
	BaseEvent
	Name    string      `json:"name"`
	Payload interface{} `json:"payload"`
}

type Event struct {
	BaseEvent
	Data interface{} `json:"data"`
}

type RunConfig struct {
	RunID     string
	ThreadID  string
	SessionID string
	UserID    string
	Model     string
}

type Handler struct {
	agent    *agent.Agent
	config   RunConfig
	events   chan Event
	mu       sync.RWMutex
	closed   bool
	runID    string
	threadID string
}

func NewHandler(a *agent.Agent, config RunConfig) *Handler {
	if config.RunID == "" {
		config.RunID = generateID("run")
	}
	if config.ThreadID == "" {
		config.ThreadID = generateID("thread")
	}

	return &Handler{
		agent:    a,
		config:   config,
		events:   make(chan Event, 256),
		runID:    config.RunID,
		threadID: config.ThreadID,
	}
}

func (h *Handler) Events() <-chan Event {
	return h.events
}

func (h *Handler) Run(ctx context.Context, messages []provider.Message, opts ...agent.RunOption) (*agent.RunOutput, error) {
	opts = append(opts, agent.WithSession(h.config.SessionID), agent.WithUser(h.config.UserID))

	h.emit(Event{
		BaseEvent: BaseEvent{
			Type:      EventRunStarted,
			Timestamp: time.Now().UnixMilli(),
			RunID:     h.runID,
			ThreadID:  h.threadID,
		},
		Data: RunStartedEvent{
			BaseEvent: BaseEvent{Type: EventRunStarted, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
			Model:     h.config.Model,
			ThreadID:  h.threadID,
			SessionID: h.config.SessionID,
		},
	})

	allOpts := make([]agent.RunOption, 0, len(opts)+2)
	allOpts = append(allOpts, agent.WithSession(h.config.SessionID), agent.WithUser(h.config.UserID))
	allOpts = append(allOpts, opts...)

	output, err := h.agent.Run(ctx, messages, allOpts...)
	if err != nil {
		h.emit(Event{
			BaseEvent: BaseEvent{
				Type:      EventRunError,
				Timestamp: time.Now().UnixMilli(),
				RunID:     h.runID,
				ThreadID:  h.threadID,
			},
			Data: RunErrorEvent{
				BaseEvent: BaseEvent{Type: EventRunError, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
				Error:     err.Error(),
				Code:      "RUN_ERROR",
			},
		})
		return nil, err
	}

	h.emit(Event{
		BaseEvent: BaseEvent{
			Type:      EventRunFinished,
			Timestamp: time.Now().UnixMilli(),
			RunID:     h.runID,
			ThreadID:  h.threadID,
		},
		Data: RunFinishedEvent{
			BaseEvent:   BaseEvent{Type: EventRunFinished, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
			Steps:       output.Steps,
			Usage:       output.Usage,
			FinalOutput: output.Message.Content,
		},
	})

	return output, nil
}

func (h *Handler) RunStream(ctx context.Context, messages []provider.Message, opts ...agent.RunOption) (<-chan Event, error) {
	opts = append(opts, agent.WithSession(h.config.SessionID), agent.WithUser(h.config.UserID))

	agentEvents, err := h.agent.RunStream(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}

	go func() {
		defer h.Close()

		h.emit(Event{
			BaseEvent: BaseEvent{
				Type:      EventRunStarted,
				Timestamp: time.Now().UnixMilli(),
				RunID:     h.runID,
				ThreadID:  h.threadID,
			},
			Data: RunStartedEvent{
				BaseEvent: BaseEvent{Type: EventRunStarted, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
				Model:     h.config.Model,
				ThreadID:  h.threadID,
				SessionID: h.config.SessionID,
			},
		})

		messageID := generateID("msg")

		for agentEvent := range agentEvents {
			switch agentEvent.Type {
			case agent.StreamEventTypeStepStart:
				h.emit(Event{
					BaseEvent: BaseEvent{
						Type:      EventStepStarted,
						Timestamp: time.Now().UnixMilli(),
						RunID:     h.runID,
						ThreadID:  h.threadID,
					},
					Data: StepStartedEvent{
						BaseEvent: BaseEvent{Type: EventStepStarted, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
						Step:      agentEvent.Step,
				 },
				})
				h.emit(Event{
					BaseEvent: BaseEvent{
						Type:      EventTextMessageStart,
						Timestamp: time.Now().UnixMilli(),
                        RunID:     h.runID,
                        ThreadID:  h.threadID,
                    },
                    Data: TextMessageStartEvent{
                        BaseEvent: BaseEvent{Type: EventTextMessageStart, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                        MessageID: messageID,
                        Role:      "assistant",
                    },
                })

            case agent.StreamEventTypeContentDelta:
                h.emit(Event{
                    BaseEvent: BaseEvent{
                        Type:      EventTextMessageContent,
                        Timestamp: time.Now().UnixMilli(),
                        RunID:     h.runID,
                        ThreadID:  h.threadID,
                    },
                    Data: TextMessageContentEvent{
                        BaseEvent: BaseEvent{Type: EventTextMessageContent, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                        MessageID: messageID,
                        Delta:     agentEvent.Delta,
                    },
                })

            case agent.StreamEventTypeToolCall:
                if agentEvent.ToolCall != nil {
                    tc := agentEvent.ToolCall
                    toolCallID := tc.ID
                    if toolCallID == "" {
                        toolCallID = generateID("tc")
                    }
                    h.emit(Event{
                        BaseEvent: BaseEvent{
                            Type:      EventToolCallStart,
                            Timestamp: time.Now().UnixMilli(),
                            RunID:     h.runID,
                            ThreadID:  h.threadID,
                        },
                        Data: ToolCallStartEvent{
                            BaseEvent:  BaseEvent{Type: EventToolCallStart, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                            ToolCallID: toolCallID,
                            ToolName:   tc.Function.Name,
                        },
                    })
                    h.emit(Event{
                        BaseEvent: BaseEvent{
                            Type:      EventToolCallArgs,
                            Timestamp: time.Now().UnixMilli(),
                            RunID:     h.runID,
                            ThreadID:  h.threadID,
                        },
                        Data: ToolCallArgsEvent{
                            BaseEvent:  BaseEvent{Type: EventToolCallArgs, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                            ToolCallID: toolCallID,
                            Delta:      string(tc.Function.Arguments),
                        },
                    })
                }

            case agent.StreamEventTypeToolResult:
                if agentEvent.ToolResult != nil {
                    result := agentEvent.ToolResult
                    h.emit(Event{
                        BaseEvent: BaseEvent{
                            Type:      EventToolCallEnd,
                            Timestamp: time.Now().UnixMilli(),
                            RunID:     h.runID,
                            ThreadID:  h.threadID,
                        },
                        Data: ToolCallEndEvent{
                            BaseEvent:  BaseEvent{Type: EventToolCallEnd, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                            ToolCallID: result.ToolName,
                            Result:     fmt.Sprintf("%v", result.Result),
                            Success:    result.Success,
                        },
                    })
                }

            case agent.StreamEventTypeStepEnd:
                h.emit(Event{
                    BaseEvent: BaseEvent{
                        Type:      EventTextMessageEnd,
                        Timestamp: time.Now().UnixMilli(),
                        RunID:     h.runID,
                        ThreadID:  h.threadID,
                    },
                    Data: TextMessageEndEvent{
                        BaseEvent: BaseEvent{Type: EventTextMessageEnd, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                        MessageID: messageID,
                    },
                })
                h.emit(Event{
                    BaseEvent: BaseEvent{
                        Type:      EventStepFinished,
                        Timestamp: time.Now().UnixMilli(),
                        RunID:     h.runID,
                        ThreadID:  h.threadID,
                    },
                    Data: StepFinishedEvent{
                        BaseEvent: BaseEvent{Type: EventStepFinished, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                        Step:      agentEvent.Step,
                        Status:    "completed",
                    },
                })
                messageID = generateID("msg")

            case agent.StreamEventTypeError:
                h.emit(Event{
                    BaseEvent: BaseEvent{
                        Type:      EventRunError,
                        Timestamp: time.Now().UnixMilli(),
                        RunID:     h.runID,
                        ThreadID:  h.threadID,
                    },
                    Data: RunErrorEvent{
                        BaseEvent: BaseEvent{Type: EventRunError, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                        Error:     agentEvent.Error.Error(),
                        Code:      "STREAM_ERROR",
                    },
                })
                return

            case agent.StreamEventTypeFinished:
                h.emit(Event{
                    BaseEvent: BaseEvent{
                        Type:      EventRunFinished,
                        Timestamp: time.Now().UnixMilli(),
                        RunID:     h.runID,
                        ThreadID:  h.threadID,
                    },
                    Data: RunFinishedEvent{
                        BaseEvent: BaseEvent{Type: EventRunFinished, Timestamp: time.Now().UnixMilli(), RunID: h.runID, ThreadID: h.threadID},
                        Steps:     agentEvent.Step,
                    },
                })
            }
        }
    }()

    return h.events, nil
}

}

func (h *Handler) emit(event Event) {
    h.mu.RLock()
    defer h.mu.RUnlock()

    if h.closed {
        return
    }

    select {
    case h.events <- event:
    default:
    }
}

}

func (h *Handler) Close() {
    h.mu.Lock()
    defer h.mu.Unlock()

    if !h.closed {
        h.closed = true
        close(h.events)
    }
}

func (h *Handler) RunID() string {
    return h.runID
}

func (h *Handler) ThreadID() string {
    return h.threadID
}

func WriteSSE(w io.Writer, event Event) error {
    data, err := json.Marshal(event.Data)
    if err != nil {
        return err
    }

    _, err = fmt.Fprintf(w, "data: %s\n\n", data)
    return err
}

func SSEHandler(h *Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        flusher, ok := w.(http.Flusher)
        if !ok {
            http.Error(w, "streaming not supported", http.StatusInternalServerError)
            return
        }

        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.Header().Set("X-Accel-Buffering", "no")

        ctx := r.Context()

        for {
            select {
            case <-ctx.Done():
                return
            case event, ok := <-h.Events():
                if !ok {
                    return
                }
                if err := WriteSSE(w, event); err != nil {
                    return
                }
                flusher.Flush()
            }
        }
    }
}

func generateID(prefix string) string {
    return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

type Builder struct {
    agent  *agent.Agent
    config RunConfig
}

func NewBuilder(a *agent.Agent) *Builder {
    return &Builder{agent: a}
}

func (b *Builder) WithRunID(id string) *Builder {
    b.config.RunID = id
    return b
}

func (b *Builder) WithThreadID(id string) *Builder {
    b.config.ThreadID = id
    return b
}

func (b *Builder) WithSessionID(id string) *Builder {
    b.config.SessionID = id
    return b
}

func (b *Builder) WithUserID(id string) *Builder {
    b.config.UserID = id
    return b
}

func (b *Builder) WithModel(model string) *Builder {
    b.config.Model = model
    return b
}

func (b *Builder) Build() *Handler {
    return NewHandler(b.agent, b.config)
}
