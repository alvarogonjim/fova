package local

import (
	"errors"
	"testing"
)

func TestDetectPrefersPodman(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()

	lookPath = func(bin string) (string, error) {
		switch bin {
		case "podman":
			return "/usr/bin/podman", nil
		case "docker":
			return "/usr/bin/docker", nil
		}
		return "", errors.New("not found")
	}

	rt := Detect()
	if rt.Kind != RuntimePodman {
		t.Errorf("Kind = %v, want RuntimePodman", rt.Kind)
	}
	if rt.Bin != "/usr/bin/podman" {
		t.Errorf("Bin = %q", rt.Bin)
	}
	if !rt.Available() {
		t.Error("Available() = false, want true")
	}
}

func TestDetectFallsBackToDocker(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()

	lookPath = func(bin string) (string, error) {
		if bin == "docker" {
			return "/usr/bin/docker", nil
		}
		return "", errors.New("not found")
	}

	rt := Detect()
	if rt.Kind != RuntimeDocker {
		t.Errorf("Kind = %v, want RuntimeDocker", rt.Kind)
	}
}

func TestDetectNoRuntime(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()

	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	rt := Detect()
	if rt.Kind != RuntimeNone {
		t.Errorf("Kind = %v, want RuntimeNone", rt.Kind)
	}
	if rt.Available() {
		t.Error("Available() = true, want false")
	}
}

func TestRuntimeKindString(t *testing.T) {
	cases := []struct {
		k    RuntimeKind
		want string
	}{
		{RuntimePodman, "podman"},
		{RuntimeDocker, "docker"},
		{RuntimeNone, "none"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("(%v).String() = %q, want %q", c.k, got, c.want)
		}
	}
}
