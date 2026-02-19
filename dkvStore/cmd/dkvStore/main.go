package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/koss756/dkvStore/api/grpc"
	"github.com/koss756/dkvStore/api/httpapi"
	"github.com/koss756/dkvStore/raft"
)

func main() {
	conf := raft.Config{ElectionTimeoutLowerBound: 150, ElectionTimeoutUpperBound: 300, HeartbeatTimeout: 100}

	var (
		id       = flag.Int("id", 0, "node id")
		grpcAddr = flag.String("grpc-addr", "", "listen address (e.g. :9000)")
		httpAddr = flag.String("http-addr", "", "listen address (e.g. :8000)")
		peers    = flag.String("peers", "", "comma-separated peer addresses")
	)
	flag.Parse()

	if *id == 0 || *grpcAddr == "" || *httpAddr == "" {
		log.Fatal("id, grpc-addr, and http-addr are required")
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

	httpServer := httpapi.NewServer(node, *httpAddr)

	// Start gRPC server (Raft RPCs)
	go func() {
		log.Printf("Starting gRPC server on %s", *grpcAddr)
		if err := grpc.StartServer(*grpcAddr, node.GetId(), node); err != nil {
			log.Fatalf("gRPC server stopped: %v", err)
		}
	}()

	// Start HTTP server (client commands)
	go func() {
		log.Printf("Starting HTTP server on %s", *httpAddr)
		if err := httpServer.Start(); err != nil {
			log.Fatalf("HTTP server stopped: %v", err)
		}
	}()

	// Start Raft node
	go node.Start()

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down...")
}
