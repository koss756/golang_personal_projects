package main

import (
	"log"
	"time"

	"github.com/koss756/dkvStore/api"
	"github.com/koss756/dkvStore/raft"
)

func main() {
	// Create Raft node
	node := raft.NewNode()

	// Start gRPC server with Raft service
	go func() {
		if err := api.StartServer(":9001", node); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)
	node.Start()

	select {}
}
