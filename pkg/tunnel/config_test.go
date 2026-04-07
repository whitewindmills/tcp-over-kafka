package tunnel

import (
	"os"
	"path/filepath"
	"testing"
)

func testConfig() Config {
	return Config{
		Broker:     "127.0.0.1:9092",
		Topic:      "tcp-over-kafka",
		NID:        "10.0.0.167",
		ListenAddr: "127.0.0.1:1234",
		Routes: map[string]Endpoint{
			"10.0.0.168:22": {NID: "10.0.0.168", EID: "22"},
		},
		Services: map[string]string{
			"22": "127.0.0.1:22",
		},
		MaxFrameSize: 1024,
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := testConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() returned error: %v", err)
	}
}

func TestConfigValidateReportsSpecificFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "listen", cfg: Config{Broker: "b", Topic: "t", NID: "p", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, Services: map[string]string{"22": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing node listen address"},
		{name: "listen invalid", cfg: Config{ListenAddr: "bad", Broker: "b", Topic: "t", NID: "p", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, Services: map[string]string{"22": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "invalid node listen address \"bad\": address bad: missing port in address"},
		{name: "broker", cfg: Config{ListenAddr: "127.0.0.1:1", Topic: "t", NID: "p", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, Services: map[string]string{"22": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing node broker address"},
		{name: "topic", cfg: Config{ListenAddr: "127.0.0.1:1", Broker: "b", NID: "p", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, Services: map[string]string{"22": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing node topic"},
		{name: "nid", cfg: Config{ListenAddr: "127.0.0.1:1", Broker: "b", Topic: "t", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, Services: map[string]string{"22": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing node nid"},
		{name: "routes", cfg: Config{ListenAddr: "127.0.0.1:1", Broker: "b", Topic: "t", NID: "p", Services: map[string]string{"22": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing node routes"},
		{name: "services", cfg: Config{ListenAddr: "127.0.0.1:1", Broker: "b", Topic: "t", NID: "p", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, MaxFrameSize: 1}, want: "missing node service mappings"},
		{name: "frame", cfg: Config{ListenAddr: "127.0.0.1:1", Broker: "b", Topic: "t", NID: "p", Routes: map[string]Endpoint{"10.0.0.168:22": {NID: "q", EID: "22"}}, Services: map[string]string{"22": "127.0.0.1:22"}}, want: "max frame size must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.validate(); err == nil || err.Error() != tt.want {
				t.Fatalf("validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "node.json")
	raw := []byte(`{
  "broker": "10.0.0.166:9092",
  "topic": "tcp-over-kafka",
  "nid": "10.0.0.167",
  "listen": "127.0.0.1:1234",
  "routes": {
    "10.0.0.168:22": {
      "nid": "10.0.0.168",
      "eid": "22"
    }
  },
  "services": {
    "22": "127.0.0.1:22"
  }
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.MaxFrameSize != DefaultMaxFrameSize {
		t.Fatalf("MaxFrameSize = %d, want %d", cfg.MaxFrameSize, DefaultMaxFrameSize)
	}
	if got := cfg.ConsumerGroup(); got != "tcp-over-kafka.node.10.0.0.167" {
		t.Fatalf("ConsumerGroup() = %q", got)
	}
	if got := cfg.ProxyEndpoint(); got != (Endpoint{NID: "10.0.0.167", EID: proxyEID}) {
		t.Fatalf("ProxyEndpoint() = %#v", got)
	}
}
