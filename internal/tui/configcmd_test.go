package tui

import (
	"strings"
	"testing"
)

func TestConfigAssetKeyValidation(t *testing.T) {
	for _, ok := range []string{"config", "models", "system"} {
		if _, valid := configAssetKey(ok); !valid {
			t.Errorf("%q should be a valid /config asset", ok)
		}
	}
	if _, valid := configAssetKey("nonsense"); valid {
		t.Error("nonsense should be rejected")
	}
}

func TestConfigAssetKeyMapsSystem(t *testing.T) {
	key, _ := configAssetKey("system")
	if key != "system" {
		t.Errorf("system maps to %q", key)
	}
}

func TestConfigUsageOnBadArg(t *testing.T) {
	if !strings.Contains(configEditUsage(), "config|models|system") {
		t.Error("usage string should list the valid assets")
	}
}
