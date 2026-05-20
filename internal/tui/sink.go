package tui

import (
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
)

// storeSink persists a session's messages to the SQLite store. It satisfies
// agent.MessageSink. Persistence is best-effort: a write error is dropped so a
// storage hiccup never breaks the chat.
type storeSink struct {
	st        *store.Store
	sessionID domain.SessionID
}

func (s storeSink) PersistMessage(role, content, toolCallID string) {
	now := time.Now().UTC()
	_ = s.st.InsertMessage(domain.Message{
		ID:         uuid.NewString(),
		SessionID:  s.sessionID,
		Role:       role,
		Content:    content,
		ToolCallID: toolCallID,
		Created:    now,
	})
	_ = s.st.TouchSession(s.sessionID, now)
}
