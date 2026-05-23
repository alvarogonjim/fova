package knowledge

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*PDBSearch)(nil)

func TestPDBSearchExecuteEmptyQuery(t *testing.T) {
	tool := NewPDBSearch()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}
