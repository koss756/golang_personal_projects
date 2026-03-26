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

	var (
		id       = flag.String("id", "", "node id (e.g. node1)")
		grpcAddr = flag.String("grpc-addr", "", "listen address (e.g. :9000)")
		httpAddr = flag.String("http-addr", "", "listen address (e.g. :8000)")
		peers    = flag.String("peers", "", "comma-separated peer addresses")
	)
	flag.Parse()

	if *id == "" || *grpcAddr == "" || *httpAddr == "" {
		log.Fatal("id, grpc-addr, and http-addr are required")
	}

	peerList := []raft.Peer{}
	if *peers != "" {
		for _, p := range strings.Split(*peers, ",") {
			parts := strings.SplitN(p, "@", 2)
			if len(parts) != 2 {
				log.Fatalf("invalid peer format %q, expected id@grpc-addr", p)
			}
			peerList = append(peerList, raft.Peer{ID: parts[0], GRPCAddr: parts[1]})
		}
	}

	conf := raft.Config{
		ID:       *id,
		GRPCAddr: *grpcAddr,
		HTTPAddr: *httpAddr,
		Peers:    peerList,

		ElectionTimeoutLowerBound: 1500,
		ElectionTimeoutUpperBound: 3000,
		HeartbeatTimeout:          1000,
	}

	peerAddresses := make([]string, 0, len(peerList))
	for _, p := range peerList {
		peerAddresses = append(peerAddresses, p.GRPCAddr)
	}

	grpcClient, err := grpc.NewGRPCClient(peerAddresses)
	if err != nil {
		log.Fatalf("failed to create gRPC client: %v", err)
	}

	node := raft.NewNode(grpcClient, conf)

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
