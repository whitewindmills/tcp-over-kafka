package tunnel

import (
	"bytes"
	"context"
	"log"
	"time"

	"github.com/segmentio/kafka-go"

	"tcp-over-kafka/pkg/frame"
)

// tunnelBus is the transport contract used by the tunnel client and server.
type tunnelBus interface {
	Send(context.Context, frame.Frame) error
	Receive(context.Context) (frame.Frame, error)
	Close() error
}

// Bus wraps the Kafka writer/reader pair used by one tunnel endpoint.
type Bus struct {
	writer *kafka.Writer
	reader *kafka.Reader
}

// NewBus creates a single-topic Kafka transport for one side of the tunnel.
func NewBus(broker, topic, group string) *Bus {
	return newBusWithStartOffset(broker, topic, group, kafka.LastOffset)
}

func newBusWithStartOffset(broker, topic, group string, startOffset int64) *Bus {
	return &Bus{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(broker),
			Topic:        topic,
			Balancer:     &kafka.Hash{},
			RequiredAcks: kafka.RequireOne,
			BatchTimeout: 10 * time.Millisecond,
			BatchSize:    1,
			Async:        false,
		},
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:     []string{broker},
			GroupID:     group,
			Topic:       topic,
			MinBytes:    1,
			MaxBytes:    1 << 20,
			MaxWait:     500 * time.Millisecond,
			StartOffset: startOffset,
		}),
	}
}

// Close shuts down the Kafka reader and writer.
func (b *Bus) Close() error {
	var err error
	if b.reader != nil {
		err = b.reader.Close()
	}
	if b.writer != nil {
		if werr := b.writer.Close(); err == nil {
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
	return b.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(frameConversationKey(f)),
		Value: raw,
		Time:  time.Now(),
	})
}

// Receive blocks until a frame arrives on the inbound Kafka topic.
func (b *Bus) Receive(ctx context.Context) (frame.Frame, error) {
	for {
		msg, err := b.reader.ReadMessage(ctx)
		if err != nil {
			return frame.Frame{}, err
		}
		decoded, err := frame.Decode(bytes.NewReader(msg.Value))
		if err != nil {
			log.Printf("discarding unreadable frame on topic %s: %v", b.reader.Config().Topic, err)
			continue
		}
		return decoded, nil
	}
}
