package main

import (
    "context"
    "log"
    "os"

    "go.temporal.io/sdk/client"
)

func main() {
    workflowID := os.Args[1]
    c, _ := client.Dial(client.Options{HostPort: "localhost:7233"})
    defer c.Close()

    err := c.SignalWorkflow(context.Background(),
        workflowID, "",
        "email-open",
        "user clicked the link",
    )
    if err != nil {
        log.Fatalln(err)
    }
    log.Println("Signal sent!")
}