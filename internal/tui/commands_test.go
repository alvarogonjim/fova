package tui

import "testing"

func TestSlashCommandsCatalogue(t *testing.T) {
	if len(slashCommands) == 0 {
		t.Fatal("slashCommands is empty")
	}
	for _, c := range slashCommands {
		if c.Name == "" || c.Description == "" {
			t.Errorf("catalogue entry has an empty field: %+v", c)
		}
	}
	for _, name := range []string{"model", "clear", "help", "quit"} {
		if len(matchCommands(name)) == 0 {
			t.Errorf("catalogue is missing %q", name)
		}
	}
}

func TestMatchCommands(t *testing.T) {
	if got := matchCommands(""); len(got) != len(slashCommands) {
		t.Errorf("empty prefix returned %d entries, want %d", len(got), len(slashCommands))
	}
	got := matchCommands("hel")
	if len(got) != 1 || got[0].Name != "help" {
		t.Errorf(`matchCommands("hel") = %+v, want [help]`, got)
	}
	if got := matchCommands("HELP"); len(got) != 1 {
		t.Errorf("matchCommands is not case-insensitive: %+v", got)
	}
	if got := matchCommands("zzz"); len(got) != 0 {
		t.Errorf("matchCommands(zzz) = %+v, want empty", got)
	}
	// A shared prefix returns every match (model + modal).
	if got := matchCommands("mod"); len(got) != 2 {
		t.Errorf(`matchCommands("mod") = %+v, want 2 (model, modal)`, got)
	}
}

// containsRow reports whether rows includes a row with the given label.
func containsRow(rows []SlashRow, label string) bool {
	for _, r := range rows {
		if r.Label == label {
			return true
		}
	}
	return false
}

func TestSlashMenuShowsPlanSubcommands(t *testing.T) {
	rows := MatchSlash("/plan ", slashCommands, nil, nil, nil)
	if !containsRow(rows, "/plan approve") {
		t.Errorf("expected /plan approve in:\n%+v", rows)
	}
	if !containsRow(rows, "/plan cancel") {
		t.Errorf("expected /plan cancel in:\n%+v", rows)
	}
}

func TestSlashMenuFiltersSubcommandsByPrefix(t *testing.T) {
	rows := MatchSlash("/plan ap", slashCommands, nil, nil, nil)
	if len(rows) != 1 || rows[0].Label != "/plan approve" {
		t.Errorf("expected only /plan approve, got: %+v", rows)
	}
}

func TestSlashMenuListsInstallableTools(t *testing.T) {
	tools := []string{"bindcraft", "proteinmpnn", "rfdiffusion"}
	rows := MatchSlash("/install ", slashCommands, tools, nil, nil)
	if !containsRow(rows, "/install bindcraft") {
		t.Errorf("expected /install bindcraft in %+v", rows)
	}
	if !containsRow(rows, "/install proteinmpnn") {
		t.Errorf("expected /install proteinmpnn in %+v", rows)
	}
	if !containsRow(rows, "/install rfdiffusion") {
		t.Errorf("expected /install rfdiffusion in %+v", rows)
	}
}

func TestSlashMenuFiltersInstallableToolsByPrefix(t *testing.T) {
	tools := []string{"bindcraft", "proteinmpnn", "rfdiffusion"}
	rows := MatchSlash("/install pr", slashCommands, tools, nil, nil)
	if len(rows) != 1 || rows[0].Label != "/install proteinmpnn" {
		t.Errorf("expected only /install proteinmpnn, got: %+v", rows)
	}
}

func TestSlashMenuListsUninstallableTools(t *testing.T) {
	tools := []string{"bindcraft", "proteinmpnn"}
	rows := MatchSlash("/uninstall ", slashCommands, tools, nil, nil)
	if !containsRow(rows, "/uninstall bindcraft") {
		t.Errorf("expected /uninstall bindcraft in %+v", rows)
	}
}

func TestSlashMenuListsModels(t *testing.T) {
	models := []string{"claude-opus-4-7", "gpt-5", "gemini-2.5-pro"}
	rows := MatchSlash("/model ", slashCommands, nil, models, nil)
	if !containsRow(rows, "/model claude-opus-4-7") {
		t.Errorf("expected /model claude-opus-4-7 in %+v", rows)
	}
	if !containsRow(rows, "/model gpt-5") {
		t.Errorf("expected /model gpt-5 in %+v", rows)
	}
}

func TestSlashMenuListsAuthProviders(t *testing.T) {
	providers := []string{"adaptyv"}
	rows := MatchSlash("/auth ", slashCommands, nil, nil, providers)
	if !containsRow(rows, "/auth adaptyv") {
		t.Errorf("expected /auth adaptyv in %+v", rows)
	}
}

func TestSlashMenuListsThemeModes(t *testing.T) {
	rows := MatchSlash("/theme ", slashCommands, nil, nil, nil)
	for _, want := range []string{"/theme auto", "/theme light", "/theme dark"} {
		if !containsRow(rows, want) {
			t.Errorf("expected %s in %+v", want, rows)
		}
	}
}

func TestSlashMenuTopLevelUnchanged(t *testing.T) {
	// Bare "/" with no trailing space lists every top-level command.
	rows := MatchSlash("/", slashCommands, nil, nil, nil)
	if len(rows) != len(slashCommands) {
		t.Errorf("bare '/' got %d rows, want %d", len(rows), len(slashCommands))
	}
	// "/pl" filters to /plan and nothing else.
	rows = MatchSlash("/pl", slashCommands, nil, nil, nil)
	if len(rows) != 1 || rows[0].Label != "/plan" {
		t.Errorf("/pl -> %+v, want only /plan", rows)
	}
	// Insert for top-level rows is "/<cmd> " (trailing space, as today).
	if rows[0].Insert != "/plan " {
		t.Errorf("/pl insert = %q, want /plan ", rows[0].Insert)
	}
}

func TestSlashMenuUnknownCommandNoRows(t *testing.T) {
	rows := MatchSlash("/zzz foo", slashCommands, nil, nil, nil)
	if len(rows) != 0 {
		t.Errorf("unknown command -> %+v, want []", rows)
	}
}

func TestSlashMenuNoTrailingSpaceNoSubcommands(t *testing.T) {
	// Without a trailing space, /plan is still just the top-level row.
	rows := MatchSlash("/plan", slashCommands, nil, nil, nil)
	if len(rows) != 1 || rows[0].Label != "/plan" {
		t.Errorf("/plan (no space) -> %+v, want only /plan", rows)
	}
	if containsRow(rows, "/plan approve") {
		t.Errorf("/plan without trailing space must not surface sub-commands")
	}
}

func TestSlashMenuInsertCompletesSubcommand(t *testing.T) {
	rows := MatchSlash("/plan ap", slashCommands, nil, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("expected exactly one row, got %+v", rows)
	}
	if rows[0].Insert != "/plan approve" {
		t.Errorf("Insert = %q, want %q", rows[0].Insert, "/plan approve")
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	rows := []SlashRow{
		{Insert: "/install bindcraft"},
		{Insert: "/install boltz2"},
	}
	got := LongestCommonPrefix(rows)
	if got != "/install b" {
		t.Errorf("LongestCommonPrefix = %q, want %q", got, "/install b")
	}
	// Unique match: prefix is the full insert.
	rows = []SlashRow{{Insert: "/plan approve"}}
	if got := LongestCommonPrefix(rows); got != "/plan approve" {
		t.Errorf("LongestCommonPrefix unique = %q, want %q", got, "/plan approve")
	}
	if got := LongestCommonPrefix(nil); got != "" {
		t.Errorf("LongestCommonPrefix(nil) = %q, want empty", got)
	}
}

func TestSlashMenuAuthProvidersFiltersByPrefix(t *testing.T) {
	providers := []string{"adaptyv", "modal"}
	rows := MatchSlash("/auth ad", slashCommands, nil, nil, providers)
	if len(rows) != 1 || rows[0].Label != "/auth adaptyv" {
		t.Errorf("/auth ad -> %+v, want only /auth adaptyv", rows)
	}
}

// TestSlashMenuInstallSnapshot is the golden snapshot for the /install
// argument list. The order matches tools.toml's alphabetical sort, which
// LoadRegistry guarantees via Registry.Tools().
func TestSlashMenuInstallSnapshot(t *testing.T) {
	tools := []string{"bindcraft", "boltz2", "chai1", "ipsae", "ligandmpnn", "proteinmpnn", "rfantibody", "rfdiffusion", "rfdiffusion2"}
	rows := MatchSlash("/install ", slashCommands, tools, nil, nil)
	want := []string{
		"/install bindcraft",
		"/install boltz2",
		"/install chai1",
		"/install ipsae",
		"/install ligandmpnn",
		"/install proteinmpnn",
		"/install rfantibody",
		"/install rfdiffusion",
		"/install rfdiffusion2",
	}
	if len(rows) != len(want) {
		t.Fatalf("/install ' ' got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].Label != w {
			t.Errorf("row %d label = %q, want %q", i, rows[i].Label, w)
		}
		if rows[i].Insert != w {
			t.Errorf("row %d insert = %q, want %q", i, rows[i].Insert, w)
		}
	}
}

// TestSlashMenuPlanSnapshot is the golden snapshot for the /plan sub-command
// list.
func TestSlashMenuPlanSnapshot(t *testing.T) {
	rows := MatchSlash("/plan ", slashCommands, nil, nil, nil)
	if len(rows) != 2 {
		t.Fatalf("/plan ' ' got %d rows, want 2: %+v", len(rows), rows)
	}
	if rows[0].Label != "/plan approve" {
		t.Errorf("row 0 = %q, want /plan approve", rows[0].Label)
	}
	if rows[1].Label != "/plan cancel" {
		t.Errorf("row 1 = %q, want /plan cancel", rows[1].Label)
	}
	if rows[0].Description == "" || rows[1].Description == "" {
		t.Errorf("sub-command rows must have a description: %+v", rows)
	}
}
