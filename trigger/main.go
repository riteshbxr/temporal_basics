package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	"go.temporal.io/sdk/client"

	workflow "temporal-cart-flow/workflow"
)

func main() {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalln("Failed to read config.json:", err)
	}

	var input workflow.WorkflowInput
	if err = json.Unmarshal(raw, &input); err != nil {
		log.Fatalln("Failed to parse config.json:", err)
	}

	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	if err != nil {
		log.Fatalln("Unable to create Temporal client:", err)
	}
	defer c.Close()

	we, err := c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        input.Workflow.ID + "-" + uuid.New().String(),
		TaskQueue: input.Workflow.TaskQueue,
	}, workflow.GenericWorkflow, input)
	if err != nil {
		log.Fatalln("Unable to start workflow:", err)
	}

	fmt.Printf("Started workflow: ID=%s RunID=%s\n", we.GetID(), we.GetRunID())

	var result string
	if err = we.Get(context.Background(), &result); err != nil {
		log.Fatalln("Workflow failed:", err)
	}
	fmt.Println("Result:")
	fmt.Println(result)
}
