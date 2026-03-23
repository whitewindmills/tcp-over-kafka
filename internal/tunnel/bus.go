package tunnel

import (
	"bytes"
	"context"
	"time"

	"github.com/segmentio/kafka-go"

	"tcp-over-kafka/internal/frame"
)

// tunnelBus is the transport contract used by the tunnel client and server.
type tunnelBus interface {
	Send(context.Context, frame.Frame) error
	Receive(context.Context) (frame.Frame, error)
	Close() error
}

// Bus wraps the Kafka writer/reader pair used by one tunnel endpoint.
type Bus struct {
	sendWriter *kafka.Writer
	recvReader *kafka.Reader
}

// NewBus creates a directional Kafka transport for one side of the tunnel.
func NewBus(broker, sendTopic, recvTopic, group string) *Bus {
	return &Bus{
		sendWriter: &kafka.Writer{
			Addr:         kafka.TCP(broker),
			Topic:        sendTopic,
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireOne,
			BatchTimeout: 10 * time.Millisecond,
			BatchSize:    1,
			Async:        false,
		},
		recvReader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:     []string{broker},
			GroupID:     group,
			Topic:       recvTopic,
			MinBytes:    1,
			MaxBytes:    1 << 20,
			MaxWait:     500 * time.Millisecond,
			StartOffset: kafka.FirstOffset,
		}),
	}
}

// Close shuts down the Kafka reader and writer.
func (b *Bus) Close() error {
	var err error
	if b.recvReader != nil {
		err = b.recvReader.Close()
	}
	if b.sendWriter != nil {
		if werr := b.sendWriter.Close(); err == nil {
			err = werr
		}
	}
	return err
}

// Send marshals one frame and publishes it to the outbound Kafka topic.
func (b *Bus) Send(ctx context.Context, f frame.Frame) error {
	raw, err := f.MarshalBinary()
	if err != nil {
		return err
	}
	return b.sendWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(f.SessionID + ":" + f.ConnectionID),
		Value: raw,
		Time:  time.Now(),
	})
}

// Receive blocks until a frame arrives on the inbound Kafka topic.
func (b *Bus) Receive(ctx context.Context) (frame.Frame, error) {
	msg, err := b.recvReader.ReadMessage(ctx)
	if err != nil {
		return frame.Frame{}, err
	}
	return frame.Decode(bytes.NewReader(msg.Value))
}
