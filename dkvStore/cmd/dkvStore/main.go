package main

import (
	"log"
	"time"

	"github.com/koss756/dkvStore/api"
	"github.com/koss756/dkvStore/raft"
)

func main() {
	// Create Raft node
	n1_peers := []string{":9001"}
	grpcClient_1, err := api.NewGRPCClient(n1_peers)
	if err != nil {
		log.Fatal(err)
	}
	n1 := raft.NewNode(n1_peers, grpcClient_1)

	n2_peers := []string{":9001"}
	grpcClient_2, err := api.NewGRPCClient(n2_peers)
	if err != nil {
		log.Fatal(err)
	}
	n2 := raft.NewNode(n2_peers, grpcClient_2)
	// Start gRPC server with Raft service
	go func() {
		if err := api.StartServer(":9000", "1", n1); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	go func() {
		if err := api.StartServer(":9001", "2", n2); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)
	n1.Start()

	select {}
}
