package main

// Kafka consumer for Temporal workflow management.
//
// Run:  go run kafka/consumer.go
//
// Topics:
//   temporal.workflow.start  → start a new workflow
//   temporal.workflow.signal → send a signal to a running workflow
//
// Start message (temporal.workflow.start):
//   { "input": <WorkflowInput JSON> }
//
// Signal message (temporal.workflow.signal):
//   { "workflow_id": "...", "signal_name": "...", "payload": "..." }

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	kafka "github.com/segmentio/kafka-go"
	"go.temporal.io/sdk/client"

	workflow "temporal-cart-flow/workflow"
)

const (
	topicStart         = "temporal.workflow.start"
	topicSignal        = "temporal.workflow.signal"
	consumerGroupStart = "temporal-consumer-start"
	consumerGroupSignal = "temporal-consumer-signal"
)

var kafkaBroker = envOrDefault("KAFKA_BROKER", "localhost:9092")

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type signalMessage struct {
	WorkflowID string `json:"workflow_id"`
	SignalName string `json:"signal_name"`
	Payload    string `json:"payload"`
}

func ensureTopics(ctx context.Context, addr string, topics ...string) {
	cl := &kafka.Client{
		Addr:    kafka.TCP(addr),
		Timeout: 15 * time.Second,
	}
	specs := make([]kafka.TopicConfig, len(topics))
	for i, t := range topics {
		specs[i] = kafka.TopicConfig{Topic: t, NumPartitions: 1, ReplicationFactor: 1}
	}
	resp, err := cl.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Addr:   kafka.TCP(addr),
		Topics: specs,
	})
	if err != nil {
		log.Fatalf("kafka: create topics request failed: %v", err)
	}
	for topic, terr := range resp.Errors {
		if terr != nil && !errors.Is(terr, kafka.TopicAlreadyExists) {
			log.Fatalf("kafka: failed to create topic %q: %v", topic, terr)
		}
	}
	log.Printf("kafka: topics ready: %v", topics)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ensureTopics(ctx, kafkaBroker, topicStart, topicSignal)

	c, err := client.Dial(client.Options{HostPort: envOrDefault("TEMPORAL_HOST", "localhost:7233")})
	if err != nil {
		log.Fatal("failed to connect to Temporal:", err)
	}
	defer c.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		consumeStart(ctx, c)
	}()

	go func() {
		defer wg.Done()
		consumeSignal(ctx, c)
	}()

	log.Printf("kafka consumer started — broker=%s", kafkaBroker)
	wg.Wait()
	log.Println("kafka consumer stopped")
}

// consumeStart reads from temporal.workflow.start and starts a workflow for each message.
func consumeStart(ctx context.Context, c client.Client) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{kafkaBroker},
		Topic:       topicStart,
		GroupID:     consumerGroupStart,
		StartOffset: kafka.FirstOffset,
	})
	defer r.Close()

	log.Printf("listening on topic %s...", topicStart)
	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Println("start: read error:", err)
			continue
		}

		var input workflow.WorkflowInput
		if err := json.Unmarshal(msg.Value, &input); err != nil {
			log.Printf("start: invalid message at offset %d: %v", msg.Offset, err)
			if err := r.CommitMessages(ctx, msg); err != nil {
				log.Println("start: commit error (bad msg):", err)
			}
			continue
		}
		if input.Workflow.TaskQueue == "" {
			log.Printf("start: missing task_queue at offset %d, skipping", msg.Offset)
			if err := r.CommitMessages(ctx, msg); err != nil {
				log.Println("start: commit error (missing task_queue):", err)
			}
			continue
		}

		wfID := input.InstanceID
		if wfID == "" {
			wfID = uuid.New().String()
		}
		we, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:        wfID,
			TaskQueue: input.Workflow.TaskQueue,
		}, workflow.GenericWorkflow, input)
		if err != nil {
			log.Println("start: failed to start workflow:", err)
			continue
		}

		log.Printf("[kafka→start] workflow_id=%s run_id=%s", we.GetID(), we.GetRunID())
		if err := r.CommitMessages(ctx, msg); err != nil {
			log.Println("start: commit error:", err)
		}
	}
}

// consumeSignal reads from temporal.workflow.signal and forwards each signal to Temporal.
func consumeSignal(ctx context.Context, c client.Client) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{kafkaBroker},
		Topic:       topicSignal,
		GroupID:     consumerGroupSignal,
		StartOffset: kafka.FirstOffset,
	})
	defer r.Close()

	log.Printf("listening on topic %s...", topicSignal)
	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Println("signal: read error:", err)
			continue
		}

		var m signalMessage
		if err := json.Unmarshal(msg.Value, &m); err != nil {
			log.Printf("signal: invalid message at offset %d: %v", msg.Offset, err)
			if err := r.CommitMessages(ctx, msg); err != nil {
				log.Println("signal: commit error (bad msg):", err)
			}
			continue
		}

		if m.WorkflowID == "" || m.SignalName == "" {
			log.Printf("signal: missing workflow_id or signal_name at offset %d, skipping", msg.Offset)
			if err := r.CommitMessages(ctx, msg); err != nil {
				log.Println("signal: commit error (missing fields):", err)
			}
			continue
		}

		if err := c.SignalWorkflow(ctx, m.WorkflowID, "", m.SignalName, m.Payload); err != nil {
			log.Printf("signal: failed to signal workflow %s: %v", m.WorkflowID, err)
			continue
		}

		log.Printf("[kafka→signal] workflow_id=%s signal=%s", m.WorkflowID, m.SignalName)
		if err := r.CommitMessages(ctx, msg); err != nil {
			log.Println("signal: commit error:", err)
		}
	}
}
