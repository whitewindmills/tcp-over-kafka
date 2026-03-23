package tunnel

import "fmt"

// topicC2S returns the client-to-server Kafka topic for one tunnel ID.
func topicC2S(id string) string {
	return fmt.Sprintf("tcp-over-kafka.%s.c2s", id)
}

// topicS2C returns the server-to-client Kafka topic for one tunnel ID.
func topicS2C(id string) string {
	return fmt.Sprintf("tcp-over-kafka.%s.s2c", id)
}
