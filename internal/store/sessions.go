package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

// InsertSession persists a new session.
func (s *Store) InsertSession(sess domain.Session) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, project_id, created, updated, model, provider)
		 VALUES (?,?,?,?,?,?)`,
		string(sess.ID), string(sess.ProjectID),
		sess.Created.UTC().Format(timeLayout), sess.Updated.UTC().Format(timeLayout),
		sess.Model, sess.Provider,
	)
	return err
}

// TouchSession updates a session's "updated" timestamp.
func (s *Store) TouchSession(id domain.SessionID, updated time.Time) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET updated=? WHERE id=?`,
		updated.UTC().Format(timeLayout), string(id),
	)
	return err
}

// GetSession returns one session by ID.
func (s *Store) GetSession(id domain.SessionID) (domain.Session, error) {
	var (
		sess             domain.Session
		created, updated string
	)
	if err := s.db.QueryRow(
		`SELECT id, project_id, created, updated, model, provider FROM sessions WHERE id=?`,
		string(id),
	).Scan(&sess.ID, &sess.ProjectID, &created, &updated, &sess.Model, &sess.Provider); err != nil {
		return domain.Session{}, err
	}
	var err error
	if sess.Created, err = parseTime(created); err != nil {
		return domain.Session{}, err
	}
	if sess.Updated, err = parseTime(updated); err != nil {
		return domain.Session{}, err
	}
	return sess, nil
}

// InsertMessage persists one message.
func (s *Store) InsertMessage(m domain.Message) error {
	var toolCalls any
	if len(m.ToolCalls) > 0 {
		raw, err := json.Marshal(m.ToolCalls)
		if err != nil {
			return err
		}
		toolCalls = string(raw)
	}
	_, err := s.db.Exec(
		`INSERT INTO messages (id, session_id, role, content, tool_calls,
		   tool_call_id, created, tokens, cost_usd)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		m.ID, string(m.SessionID), m.Role, m.Content, toolCalls,
		nullStr(m.ToolCallID), m.Created.UTC().Format(timeLayout), m.Tokens, m.CostUSD,
	)
	return err
}

// ListMessages returns a session's messages in chronological order.
func (s *Store) ListMessages(sessionID domain.SessionID) ([]domain.Message, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content, tool_calls, tool_call_id,
		        created, tokens, cost_usd
		 FROM messages WHERE session_id=? ORDER BY created, rowid`,
		string(sessionID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var (
			m                     domain.Message
			toolCalls, toolCallID sql.NullString
			created               string
		)
		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.Role, &m.Content, &toolCalls,
			&toolCallID, &created, &m.Tokens, &m.CostUSD,
		); err != nil {
			return nil, err
		}
		if toolCalls.Valid && toolCalls.String != "" {
			if err := json.Unmarshal([]byte(toolCalls.String), &m.ToolCalls); err != nil {
				return nil, err
			}
		}
		m.ToolCallID = toolCallID.String
		if m.Created, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
