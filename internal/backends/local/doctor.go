package local

import (
	"fmt"
	"os"
	"strings"
)

// ToolLine is one tool's row in the diagnostic report.
type ToolLine struct {
	Name        string
	Installed   bool
	Version     string
	Image       string
	LegacyVenv  bool // recipe is container-mode but a legacy install dir exists
	Containered bool // recipe has an ImageTag (container-mode)
}

// Report is the result of a fova local-backend diagnostic.
type Report struct {
	UVFound         bool
	UVPath          string
	Runtime         Runtime
	BaseImageCached bool
	NvidiaToolkit   bool
	Tools           []ToolLine
}

// Diagnose inspects the container runtime, uv availability, and the install
// status of every tool. Probes the container runtime via the Installer's
// detected Runtime; falls back gracefully when no runtime is available.
func Diagnose(reg *Registry, inst *Installer) Report {
	rep := Report{Runtime: inst.runtime}
	rep.UVPath, rep.UVFound = UVPath()
	if rep.Runtime.Available() {
		cached, _ := rep.Runtime.ImageExists(BaseImage)
		rep.BaseImageCached = cached
		if info, err := rep.Runtime.Info(); err == nil {
			rep.NvidiaToolkit = strings.Contains(info, "nvidia")
		}
	}
	for _, rec := range reg.Tools() {
		st := inst.Status(rec.Name)
		line := ToolLine{
			Name:        rec.Name,
			Installed:   st.Installed,
			Version:     st.Version,
			Image:       st.Image,
			Containered: rec.ImageTag != "",
		}
		// Legacy migration hint: container-mode recipe with no image built,
		// but the legacy install dir still sits on disk.
		if rec.ImageTag != "" && !st.Installed && rec.InstallDir != "" {
			if fi, err := os.Stat(rec.InstallDir); err == nil && fi.IsDir() {
				line.LegacyVenv = true
			}
		}
		rep.Tools = append(rep.Tools, line)
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
	b.WriteString("Container runtime\n")
	if r.Runtime.Available() {
		fmt.Fprintf(&b, "  ok  %s: %s\n", r.Runtime.Kind, r.Runtime.Bin)
		if r.NvidiaToolkit {
			b.WriteString("  ok  nvidia-container-toolkit\n")
		} else {
			b.WriteString("  !!  nvidia-container-toolkit not detected — GPU tools will not work\n")
		}
		if r.BaseImageCached {
			fmt.Fprintf(&b, "  ok  base image %s cached\n", BaseImage)
		} else {
			fmt.Fprintf(&b, "  ──  base image %s not cached (pulled on first /install)\n", BaseImage)
		}
	} else {
		b.WriteString("  ──  no container runtime — install podman or docker\n")
	}
	b.WriteString("\nSystem\n")
	if r.UVFound {
		fmt.Fprintf(&b, "  ok  uv: %s\n", r.UVPath)
	} else {
		b.WriteString("  ──  uv: not installed (fova installs it on first use)\n")
	}
	b.WriteString("\nLocal protein tools\n")
	for _, t := range r.Tools {
		switch {
		case t.Installed:
			version := t.Version
			if version == "" {
				version = t.Image
			}
			fmt.Fprintf(&b, "  ok  %-14s %s\n", t.Name, version)
		case t.LegacyVenv:
			fmt.Fprintf(&b, "  ──  %-14s legacy venv install detected — run /install %s to migrate\n",
				t.Name, t.Name)
		default:
			fmt.Fprintf(&b, "  ──  %-14s not installed (run: /install %s)\n",
				t.Name, t.Name)
		}
	}
	return b.String()
}
