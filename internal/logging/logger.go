package logging

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

type Field map[string]any

type ShardRef struct {
	Owner  string `json:"owner,omitempty"`
	BlobID string `json:"blob_id,omitempty"`
	Index  int    `json:"index"`
}

type Event struct {
	Time        time.Time `json:"time"`
	Level       string    `json:"level"`
	Event       string    `json:"event"`
	Component   string    `json:"component,omitempty"`
	Message     string    `json:"message,omitempty"`
	PeerID      string    `json:"peer_id,omitempty"`
	ShardRef    *ShardRef `json:"shard_ref,omitempty"`
	OperationID string    `json:"operation_id,omitempty"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
	Error       string    `json:"error,omitempty"`
	Fields      Field     `json:"fields,omitempty"`
}

type Logger struct {
	mu        *sync.Mutex
	out       io.Writer
	component string
	now       func() time.Time
}

func New(out io.Writer, component string) *Logger {
	return &Logger{
		mu:        &sync.Mutex{},
		out:       out,
		component: component,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (l *Logger) WithComponent(component string) *Logger {
	if l == nil {
		return nil
	}
	return &Logger{mu: l.mu, out: l.out, component: component, now: l.now}
}

func (l *Logger) Info(event, message string, fields Field) {
	l.write(Event{Level: "info", Event: event, Message: message, Fields: fields})
}

func (l *Logger) Warn(event, message, errorCode string, err error, fields Field) {
	e := Event{Level: "warn", Event: event, Message: message, ErrorCode: errorCode, Fields: fields}
	if err != nil {
		e.Error = err.Error()
	}
	l.write(e)
}

func (l *Logger) Error(event, message, errorCode string, err error, fields Field) {
	e := Event{Level: "error", Event: event, Message: message, ErrorCode: errorCode, Fields: fields}
	if err != nil {
		e.Error = err.Error()
	}
	l.write(e)
}

func (l *Logger) Write(event Event) {
	l.write(event)
}

func (l *Logger) write(event Event) {
	if l == nil || l.out == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = l.now()
	}
	if event.Level == "" {
		event.Level = "info"
	}
	if event.Component == "" {
		event.Component = l.component
	}
	if len(event.Fields) == 0 {
		event.Fields = nil
	}
	if l.mu == nil {
		l.mu = &sync.Mutex{}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_ = json.NewEncoder(l.out).Encode(event)
}
