package conf

import "testing"

func TestConfig(t *testing.T) {
	cfg := Default()
	cfg.Write("config.yaml")
}
