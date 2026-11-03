package config

import "testing"

func TestConfigDefaults(t *testing.T) {
	c, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != "./data" || c.Listen != "127.0.0.1:5000" || c.PublicURL != "http://localhost:5000" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}
func TestConfigRejectUnsafe(t *testing.T) {
	for _, args := range [][]string{{"--public-url", "http://user:pass@localhost:5000"}, {"--listen", "0.0.0.0:5000"}, {"--public-url", "http://localhost:5000/path"}} {
		if _, err := Parse(args); err == nil {
			t.Errorf("accepted unsafe arguments %q", args)
		}
	}
}
