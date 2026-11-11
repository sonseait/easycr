package config

import "testing"

func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "")
}
func TestPort(t *testing.T) {
	for _, port := range []string{"", "8080", "1", "65535"} {
		t.Run("valid_"+port, func(t *testing.T) {
			t.Setenv("PORT", port)
			c, err := Parse(nil)
			if port == "" {
				port = "5000"
			}
			if err != nil || c.Listen != "0.0.0.0:"+port {
				t.Fatalf("listen=%s err=%v", c.Listen, err)
			}
		})
	}
	for _, port := range []string{"0", "65536", "-1", "+80", "abc", "8080 ", "127.0.0.1:8080"} {
		t.Run("invalid_"+port, func(t *testing.T) {
			t.Setenv("PORT", port)
			if _, err := Parse(nil); err == nil {
				t.Fatal("invalid PORT accepted")
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	clearEnv(t)
	c, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != "./data" || c.Listen != "0.0.0.0:5000" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestConfigRejectArguments(t *testing.T) {
	clearEnv(t)
	if _, err := Parse([]string{"--data", "old"}); err == nil {
		t.Fatal("legacy arguments accepted")
	}
}
