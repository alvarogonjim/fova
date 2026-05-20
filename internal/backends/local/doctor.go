package local

import (
	"fmt"
	"strings"
)

// ToolLine is one tool's row in the diagnostic report.
type ToolLine struct {
	Name      string
	Installed bool
	Version   string
}

// Report is the result of a Proteus local-backend diagnostic.
type Report struct {
	UVFound bool
	UVPath  string
	Tools   []ToolLine
}

// Diagnose inspects uv availability and the install status of every tool.
func Diagnose(reg *Registry, inst *Installer) Report {
	rep := Report{}
	rep.UVPath, rep.UVFound = UVPath()
	for _, rec := range reg.Tools() {
		st := inst.Status(rec.Name)
		rep.Tools = append(rep.Tools, ToolLine{
			Name:      rec.Name,
			Installed: st.Installed,
			Version:   st.Version,
		})
	}
	return rep
}

// toolInstalled reports whether a named tool is installed per the report.
func (r Report) toolInstalled(name string) bool {
	for _, t := range r.Tools {
		if t.Name == name {
			return t.Installed
		}
	}
	return false
}

// String renders the report as human-readable diagnostic text.
func (r Report) String() string {
	var b strings.Builder
	b.WriteString("System\n")
	if r.UVFound {
		fmt.Fprintf(&b, "  ok  uv: %s\n", r.UVPath)
	} else {
		b.WriteString("  --  uv: not installed (Proteus installs it on first use)\n")
	}
	b.WriteString("\nLocal protein tools\n")
	for _, t := range r.Tools {
		if t.Installed {
			fmt.Fprintf(&b, "  ok  %-14s %s\n", t.Name, t.Version)
		} else {
			fmt.Fprintf(&b, "  --  %-14s not installed (run: /install %s)\n",
				t.Name, t.Name)
		}
	}
	return b.String()
}
