package main

import (
	"fmt"
	"log"
	"time"

	"github.com/koss756/dkvStore/api"
	"github.com/koss756/dkvStore/raft"
)

func InitCluster(numServers int, startPort int) ([]*raft.Node, error) {
	if numServers < 1 {
		return nil, fmt.Errorf("number of servers must be at least 1")
	}

	// Build list of all server addresses
	allAddrs := make([]string, numServers)
	for i := 0; i < numServers; i++ {
		allAddrs[i] = fmt.Sprintf(":%d", startPort+i)
	}

	// Create nodes
	nodes := make([]*raft.Node, numServers)
	for i := 0; i < numServers; i++ {
		// Build peer list (all addresses except this node's address)
		peers := make([]string, 0, numServers-1)
		for j := 0; j < numServers; j++ {
			if i != j {
				peers = append(peers, allAddrs[j])
			}
		}

		// Create gRPC client directly - it already implements raft.Client
		grpcClient, err := api.NewGRPCClient(peers)
		if err != nil {
			return nil, fmt.Errorf("failed to create gRPC client for node %d: %w", i, err)
		}

		// Create node with ID starting from 1
		nodeID := i + 1
		nodes[i] = raft.NewNode(nodeID, peers, grpcClient)

		// Start gRPC server for this node in a goroutine
		nodeAddr := allAddrs[i]
		nodeIdx := i
		go func(addr string, idx int) {
			if err := api.StartServer(addr, nodes[idx].GetId(), nodes[idx]); err != nil {
				log.Printf("Server on %s (node %d) stopped: %v", addr, nodes[idx].GetId(), err)
			}
		}(nodeAddr, nodeIdx)
	}

	// Give servers time to start
	time.Sleep(2 * time.Second)

	return nodes, nil
}

func main() {
	// Create a 3-node cluster starting at port 9000
	nodes, err := InitCluster(5, 9000)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Cluster initialized with %d nodes\n", len(nodes))
	for _, node := range nodes {
		node.Start()
	}

	select {}
}
