package main

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"temporal-cart-flow/activities"
	workflow "temporal-cart-flow/workflow"
)

func main() {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalln("Failed to read config.json:", err)
	}
	var cfg struct {
		Workflow struct {
			TaskQueue string `json:"task_queue"`
		} `json:"workflow"`
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		log.Fatalln("Failed to parse config.json:", err)
	}

	c, err := client.Dial(client.Options{HostPort: "localhost:7233"})
	if err != nil {
		log.Fatalln("Unable to create Temporal client:", err)
	}
	defer c.Close()

	w := worker.New(c, cfg.Workflow.TaskQueue, worker.Options{
		DeadlockDetectionTimeout: 5 * time.Second,
	})

	w.RegisterWorkflow(workflow.GenericWorkflow)
	w.RegisterActivity(activities.WaitActivity)
	w.RegisterActivity(activities.WaitForEventActivity)
	w.RegisterActivity(activities.SendEmailActivity)

	if err = w.Run(worker.InterruptCh()); err != nil {
		log.Fatalln("Unable to start worker:", err)
	}
}
