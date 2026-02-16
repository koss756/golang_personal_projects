package main

import (
	"flag"
	"log"
	"strings"

	"github.com/koss756/dkvStore/api/grpc"
	"github.com/koss756/dkvStore/raft"
)

func main() {
	conf := raft.Config{ElectionTimeoutLowerBound: 150, ElectionTimeoutUpperBound: 300, HeartbeatTimeout: 100}

	var (
		id    = flag.Int("id", 0, "node id")
		addr  = flag.String("addr", "", "listen address (e.g. :9000)")
		peers = flag.String("peers", "", "comma-separated peer addresses")
	)
	flag.Parse()

	if *id == 0 || *addr == "" {
		log.Fatal("id and addr are required")
	}

	peerList := []string{}
	if *peers != "" {
		peerList = strings.Split(*peers, ",")
	}

	grpcClient, err := grpc.NewGRPCClient(peerList)
	if err != nil {
		log.Fatalf("failed to create gRPC client: %v", err)
	}

	node := raft.NewNode(*id, peerList, grpcClient, conf)

	go func() {
		if err := grpc.StartServer(*addr, node.GetId(), node); err != nil {
			log.Fatalf("server stopped: %v", err)
		}
	}()

	go node.Start()
	select {}
}
