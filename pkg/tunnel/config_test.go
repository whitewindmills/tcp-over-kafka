package tunnel

import "testing"

func TestClientConfigValidate(t *testing.T) {
	t.Parallel()

	cfg := ClientConfig{
		ListenAddr:   "127.0.0.1:1234",
		Broker:       "127.0.0.1:9092",
		Topic:        "tcp-over-kafka",
		ClientGroup:  "client-group",
		PlatformID:   "10.0.0.167",
		DeviceID:     "client-a",
		Routes:       map[string]Endpoint{"10.0.0.168:22": {PlatformID: "10.0.0.168", DeviceID: "ssh"}},
		MaxFrameSize: 1024,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() returned error: %v", err)
	}
}

func TestClientConfigValidateReportsSpecificFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  ClientConfig
		want string
	}{
		{name: "listen", cfg: ClientConfig{Broker: "b", Topic: "t", ClientGroup: "g", PlatformID: "p", DeviceID: "d", Routes: map[string]Endpoint{"x:1": {PlatformID: "p", DeviceID: "d"}}, MaxFrameSize: 1}, want: "missing client listen address"},
		{name: "broker", cfg: ClientConfig{ListenAddr: "l", Topic: "t", ClientGroup: "g", PlatformID: "p", DeviceID: "d", Routes: map[string]Endpoint{"x:1": {PlatformID: "p", DeviceID: "d"}}, MaxFrameSize: 1}, want: "missing client broker address"},
		{name: "topic", cfg: ClientConfig{ListenAddr: "l", Broker: "b", ClientGroup: "g", PlatformID: "p", DeviceID: "d", Routes: map[string]Endpoint{"x:1": {PlatformID: "p", DeviceID: "d"}}, MaxFrameSize: 1}, want: "missing client topic"},
		{name: "group", cfg: ClientConfig{ListenAddr: "l", Broker: "b", Topic: "t", PlatformID: "p", DeviceID: "d", Routes: map[string]Endpoint{"x:1": {PlatformID: "p", DeviceID: "d"}}, MaxFrameSize: 1}, want: "missing client consumer group"},
		{name: "routes", cfg: ClientConfig{ListenAddr: "l", Broker: "b", Topic: "t", ClientGroup: "g", PlatformID: "p", DeviceID: "d", MaxFrameSize: 1}, want: "missing client routes"},
		{name: "frame", cfg: ClientConfig{ListenAddr: "l", Broker: "b", Topic: "t", ClientGroup: "g", PlatformID: "p", DeviceID: "d", Routes: map[string]Endpoint{"x:1": {PlatformID: "p", DeviceID: "d"}}}, want: "max frame size must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.validate(); err == nil || err.Error() != tt.want {
				t.Fatalf("validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestServerConfigValidateReportsSpecificFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  ServerConfig
		want string
	}{
		{name: "broker", cfg: ServerConfig{Topic: "t", ServerGroup: "g", PlatformID: "p", Services: map[string]string{"ssh": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing server broker address"},
		{name: "topic", cfg: ServerConfig{Broker: "b", ServerGroup: "g", PlatformID: "p", Services: map[string]string{"ssh": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing server topic"},
		{name: "group", cfg: ServerConfig{Broker: "b", Topic: "t", PlatformID: "p", Services: map[string]string{"ssh": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing server consumer group"},
		{name: "platform", cfg: ServerConfig{Broker: "b", Topic: "t", ServerGroup: "g", Services: map[string]string{"ssh": "127.0.0.1:22"}, MaxFrameSize: 1}, want: "missing server platform ID"},
		{name: "services", cfg: ServerConfig{Broker: "b", Topic: "t", ServerGroup: "g", PlatformID: "p", MaxFrameSize: 1}, want: "missing server service mappings"},
		{name: "frame", cfg: ServerConfig{Broker: "b", Topic: "t", ServerGroup: "g", PlatformID: "p", Services: map[string]string{"ssh": "127.0.0.1:22"}}, want: "max frame size must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.validate(); err == nil || err.Error() != tt.want {
				t.Fatalf("validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}
