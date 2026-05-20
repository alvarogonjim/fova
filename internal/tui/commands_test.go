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
