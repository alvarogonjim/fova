package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/replay"
	"github.com/alvarogonjim/fova/internal/store"
)

// newExportCmd builds the `fova export <session-id> <out.json>` command.
// Export is read-only: it opens the workspace store, walks the session's
// recorded messages, and writes a replay.Document to out.
func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <session-id> <out.json>",
		Short: "Export a recorded session as a replay JSON document (read-only)",
		Long: `Write the named session as a normalised replay JSON document.

This command never makes LLM calls and never modifies the store; it only
reads. The resulting file can be played back with 'fova replay'.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, err := defaultWorkspace()
			if err != nil {
				return err
			}
			st, err := store.Open(filepath.Join(workspace, "workspace.db"))
			if err != nil {
				return err
			}
			defer st.Close()
			return runExport(st, args[0], args[1])
		},
	}
}

// runExport transforms one session into a replay.Document and writes it to
// outPath. It is the test entry point: the cobra command is a thin wrapper.
func runExport(st *store.Store, sessionID, outPath string) error {
	sess, err := st.GetSession(domain.SessionID(sessionID))
	if err != nil {
		return fmt.Errorf("get session %q: %w", sessionID, err)
	}
	msgs, err := st.ListMessages(sess.ID)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	doc := &replay.Document{
		SessionID: string(sess.ID),
		Started:   sess.Created,
		Model:     sess.Model,
		Events:    messagesToEvents(msgs),
	}
	return doc.Write(outPath)
}

// messagesToEvents transforms the stored messages of a session into the
// ordered event stream the replay format expects. See the SP-F plan for the
// mapping rules.
func messagesToEvents(msgs []domain.Message) []replay.Event {
	events := make([]replay.Event, 0, len(msgs))
	// toolNames maps tool-call IDs to their tool name, so a later "tool" role
	// message can recover the name from its ToolCallID.
	toolNames := map[string]string{}

	for i, m := range msgs {
		switch m.Role {
		case "user":
			events = append(events, replay.Event{
				Kind: replay.KindUserMsg,
				TS:   m.Created,
				Text: m.Content,
			})
		case "assistant":
			if m.Content != "" {
				events = append(events, replay.Event{
					Kind: replay.KindAgentText,
					TS:   m.Created,
					Text: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				toolNames[tc.ID] = tc.Name
				events = append(events, replay.Event{
					Kind:  replay.KindToolStart,
					TS:    m.Created,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
			if turnEndsAt(msgs, i) {
				events = append(events, replay.Event{
					Kind: replay.KindTurnDone,
					TS:   m.Created,
				})
			}
		case "tool":
			events = append(events, replay.Event{
				Kind:    replay.KindToolResult,
				TS:      m.Created,
				Name:    toolNames[m.ToolCallID],
				Display: m.Content,
			})
		}
	}
	return events
}

// turnEndsAt reports whether the assistant message at index i terminates a
// turn — i.e. it has no tool calls, or the next message is not a tool result
// (the live agent loop always answers a tool call with a tool role next).
func turnEndsAt(msgs []domain.Message, i int) bool {
	if len(msgs[i].ToolCalls) == 0 {
		return true
	}
	if i+1 >= len(msgs) {
		return true
	}
	return msgs[i+1].Role != "tool"
}
