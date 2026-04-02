package tunnel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"tcp-over-kafka/pkg/frame"
)

func testRepoRoot(tb testing.TB) string {
	tb.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtime caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func loadProjectEnv(tb testing.TB) map[string]string {
	tb.Helper()

	path := filepath.Join(testRepoRoot(tb), "hack", ".env.local")
	cmd := exec.Command("bash", "-lc", fmt.Sprintf("set -a; source %q; set +a; env -0", path))
	raw, err := cmd.Output()
	if err != nil {
		tb.Fatalf("source %s: %v", path, err)
	}

	env := make(map[string]string)
	for _, entry := range bytes.Split(raw, []byte{0}) {
		line := strings.TrimSpace(string(entry))
		if line == "" {
			continue
		}
		if i := strings.Index(line, "="); i >= 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])
			env[key] = val
		}
	}
	return env
}

func brokerAddrFromEnv(env map[string]string) string {
	for _, key := range []string{"BROKER_ADDR", "KAFKA_BROKER"} {
		if v := strings.TrimSpace(env[key]); v != "" {
			return v
		}
	}
	return ""
}

func normalizeBrokerAddr(addr string) string {
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") {
		return addr + ":9092"
	}
	if strings.Count(addr, ":") > 1 {
		return "[" + addr + "]:9092"
	}
	return addr + ":9092"
}

func brokerReachable(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return err
	}
	return conn.Close()
}

func brokerRemoteSpec(env map[string]string) string {
	host := strings.TrimSpace(env["BROKER_SSH_HOST"])
	if host == "" {
		return ""
	}
	user := strings.TrimSpace(env["BROKER_SSH_USER"])
	if user == "" {
		user = strings.TrimSpace(env["REMOTE_SSH_USER"])
	}
	if user == "" {
		user = "root"
	}
	return fmt.Sprintf("%s@%s", user, host)
}

func brokerPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "9092"
	}
	return port
}

func remoteBrokerExec(env map[string]string, remoteCommand string) ([]byte, error) {
	remote := brokerRemoteSpec(env)
	if remote == "" {
		return nil, errors.New("missing broker ssh host")
	}

	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
	}
	remoteShell := fmt.Sprintf("bash -lc %q", remoteCommand)

	if auth := strings.TrimSpace(env["SSH_AUTH"]); auth != "" {
		sshArgs = append(sshArgs, "-o", "BatchMode=yes", "-i", auth, remote, remoteShell)
		return exec.Command("ssh", sshArgs...).CombinedOutput()
	}
	if password := strings.TrimSpace(env["SSH_PASSWORD"]); password != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			return nil, err
		}
		sshArgs = append(sshArgs,
			"-o", "BatchMode=no",
			"-o", "NumberOfPasswordPrompts=1",
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
			remote,
			remoteShell,
		)
		cmd := exec.Command("sshpass", append([]string{"-e", "ssh"}, sshArgs...)...)
		cmd.Env = append(cmd.Environ(), "SSHPASS="+password)
		return cmd.CombinedOutput()
	}

	sshArgs = append(sshArgs, "-o", "BatchMode=yes", remote, remoteShell)
	return exec.Command("ssh", sshArgs...).CombinedOutput()
}

func ensureTopicWithRemoteCLI(tb testing.TB, env map[string]string, broker, topic string) bool {
	tb.Helper()

	command := fmt.Sprintf(
		"docker exec tcp-over-kafka-broker /opt/kafka/bin/kafka-topics.sh --bootstrap-server 127.0.0.1:%s --create --if-not-exists --topic %q --partitions 1 --replication-factor 1",
		brokerPort(broker),
		topic,
	)
	output, err := remoteBrokerExec(env, command)
	if err != nil {
		tb.Logf("remote broker topic creation failed: %v: %s", err, strings.TrimSpace(string(output)))
		return false
	}
	return true
}

func ensureTopic(tb testing.TB, broker, topic string) {
	tb.Helper()
	env := loadProjectEnv(tb)

	conn, err := kafka.DialContext(context.Background(), "tcp", broker)
	if err != nil {
		tb.Fatalf("dial broker: %v", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err == nil {
		controllerConn, dialErr := kafka.Dial("tcp", net.JoinHostPort(controller.Host, fmt.Sprint(controller.Port)))
		if dialErr == nil {
			defer controllerConn.Close()
			err = controllerConn.CreateTopics(kafka.TopicConfig{
				Topic:             topic,
				NumPartitions:     1,
				ReplicationFactor: 1,
			})
			if err == nil || strings.Contains(err.Error(), "Topic with this name already exists") {
				err = nil
			}
		} else {
			err = dialErr
		}
	}
	if err != nil {
		if !ensureTopicWithRemoteCLI(tb, env, broker, topic) {
			tb.Fatalf("controller: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
		return
	}

	for i := 0; i < 20; i++ {
		if _, err := conn.ReadPartitions(topic); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	tb.Fatalf("topic %s never became readable", topic)
}

func TestRouteAndServiceResolution(t *testing.T) {
	t.Parallel()

	routes := map[string]Endpoint{
		"10.0.0.1:22": {
			PlatformID: "10.0.0.168",
			DeviceID:   "sshd",
		},
		"10.0.0.2:443": {
			PlatformID: "10.0.0.168",
			DeviceID:   "httpsd",
		},
	}
	services := map[string]string{
		"sshd":   "127.0.0.1:2222",
		"httpsd": "127.0.0.1:443",
	}

	if got, ok := resolveClientRoute(routes, "10.0.0.1:22"); !ok || got.DeviceID != "sshd" {
		t.Fatalf("route resolution mismatch: ok=%v got=%#v", ok, got)
	}
	if _, ok := resolveClientRoute(routes, "10.0.0.9:22"); ok {
		t.Fatal("expected missing route")
	}

	if got, ok := resolveServerService(services, "httpsd"); !ok || got != "127.0.0.1:443" {
		t.Fatalf("service resolution mismatch: ok=%v got=%q", ok, got)
	}
	if _, ok := resolveServerService(services, "missing"); ok {
		t.Fatal("expected missing service")
	}
}

func TestFrameConversationKeyIgnoresDirection(t *testing.T) {
	t.Parallel()

	left := Endpoint{PlatformID: "10.0.0.167", DeviceID: "client-a"}
	right := Endpoint{PlatformID: "10.0.0.168", DeviceID: "server-a"}
	first := conversationKey(left, right, "conn-1")
	second := conversationKey(right, left, "conn-1")
	if first != second {
		t.Fatalf("conversation key should be direction-agnostic: %q != %q", first, second)
	}
}

func TestProjectEnvLoadersAreTolerant(t *testing.T) {
	t.Parallel()

	env := loadProjectEnv(t)
	if len(env) == 0 {
		t.Fatal("expected hack/.env.local to contain entries")
	}
}

func TestNormalizeBrokerAddr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "10.0.0.1", want: "10.0.0.1:9092"},
		{in: "10.0.0.1:19092", want: "10.0.0.1:19092"},
		{in: "[::1]", want: "[::1]:9092"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeBrokerAddr(tc.in); got != tc.want {
				t.Fatalf("normalizeBrokerAddr(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBrokerAddrFromEnvPrefersExplicitKey(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"KAFKA_BROKER": "10.0.0.1",
		"BROKER_ADDR":  "10.0.0.2:9092",
	}
	if got := brokerAddrFromEnv(env); got != "10.0.0.2:9092" {
		t.Fatalf("brokerAddrFromEnv = %q", got)
	}
}

func TestKafkaSingleTopicRoundTripFromLocalEnv(t *testing.T) {
	env := loadProjectEnv(t)
	broker := normalizeBrokerAddr(brokerAddrFromEnv(env))
	if broker == "" {
		t.Skip("no broker configured in hack/.env.local")
	}
	if err := brokerReachable(broker); err != nil {
		t.Skipf("broker unreachable at %s: %v", broker, err)
	}

	topicPrefix := strings.TrimSpace(env["KAFKA_TOPIC"])
	if topicPrefix == "" {
		topicPrefix = "tcp-over-kafka-plan"
	}
	topic := fmt.Sprintf(
		"%s.%s.%d",
		topicPrefix,
		strings.ReplaceAll(t.Name(), "/", "-"),
		time.Now().UnixNano(),
	)
	ensureTopic(t, broker, topic)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sender := newBusWithStartOffset(
		broker,
		topic,
		"single-topic-sender-"+strings.ReplaceAll(t.Name(), "/", "-"),
		kafka.FirstOffset,
	)
	defer sender.Close()
	receiver := newBusWithStartOffset(
		broker,
		topic,
		"single-topic-receiver-"+strings.ReplaceAll(t.Name(), "/", "-"),
		kafka.FirstOffset,
	)
	defer receiver.Close()

	want := frame.Frame{
		Kind:                  frame.KindOpen,
		SourcePlatformID:      "10.0.0.167",
		SourceDeviceID:        "client-proc-42",
		DestinationPlatformID: "10.0.0.168",
		DestinationDeviceID:   "server-proc-7",
		ConnectionID:          "single-topic-connection",
		Payload:               []byte(`{"sourcePlatformID":"10.0.0.167","sourceDeviceID":"client-proc-42","destinationPlatformID":"10.0.0.168","destinationDeviceID":"server-proc-7"}`),
	}

	if err := sender.Send(ctx, want); err != nil {
		t.Fatalf("send frame: %v", err)
	}

	got, err := receiver.Receive(ctx)
	if err != nil {
		t.Fatalf("receive frame: %v", err)
	}
	if got.Kind != want.Kind {
		t.Fatalf("unexpected frame kind: %v", got.Kind)
	}
	if got.SourcePlatformID != want.SourcePlatformID ||
		got.SourceDeviceID != want.SourceDeviceID ||
		got.DestinationPlatformID != want.DestinationPlatformID ||
		got.DestinationDeviceID != want.DestinationDeviceID ||
		got.ConnectionID != want.ConnectionID {
		t.Fatalf("unexpected frame identity: %#v", got)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("payload mismatch: %q != %q", got.Payload, want.Payload)
	}
}

func TestServerOpenSessionReturnsKindErrorForUnknownService(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	clientBus, serverBus := newTestBusPair()
	clientSessions := newClientRegistry()
	serverSessions := newServerRegistry()

	done := make(chan error, 1)
	go func() {
		done <- nodeReceiveLoop(ctx, serverBus, clientSessions, serverSessions, Config{
			Broker:       "127.0.0.1:9092",
			Topic:        "tcp-over-kafka",
			PlatformID:   "10.0.0.168",
			ListenAddr:   "127.0.0.1:12345",
			Routes:       map[string]Endpoint{"10.0.0.167:22": {PlatformID: "10.0.0.167", DeviceID: "ssh"}},
			Services:     map[string]string{},
			MaxFrameSize: 128,
		}, dialerForHandlers(map[string]pipeHandler{}))
	}()

	err := clientBus.Send(ctx, frame.Frame{
		Kind:                  frame.KindOpen,
		SourcePlatformID:      "10.0.0.167",
		SourceDeviceID:        "client-a",
		DestinationPlatformID: "10.0.0.168",
		DestinationDeviceID:   "missing-service",
		ConnectionID:          "conn-1",
	})
	if err != nil {
		t.Fatalf("send open: %v", err)
	}

	reply, err := clientBus.Receive(ctx)
	if err != nil {
		t.Fatalf("receive error frame: %v", err)
	}
	if reply.Kind != frame.KindError {
		t.Fatalf("expected kind error, got %v", reply.Kind)
	}
	if reply.Err != `unknown destination: "missing-service"` {
		t.Fatalf("unexpected error text: %q", reply.Err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("server loop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server loop did not exit")
	}
}
