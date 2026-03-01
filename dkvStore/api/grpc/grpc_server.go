package grpc

import (
	"context"
	"fmt"
	stdlog "log"
	"net"

	"github.com/koss756/dkvStore/api/grpc/log"
	"github.com/koss756/dkvStore/api/grpc/vote"
	"github.com/koss756/dkvStore/raft"
	"github.com/koss756/dkvStore/types"
	"google.golang.org/grpc"
)

type GRPCServer struct {
	vote.UnimplementedVoteServiceServer
	log.UnimplementedLogServiceServer
	nodeID      string
	raftService raft.Server
}

func NewGRPCServer(nodeId string, raftService raft.Server) *GRPCServer {
	return &GRPCServer{nodeID: nodeId, raftService: raftService}
}

func (s *GRPCServer) RequestVote(ctx context.Context, req *vote.RequestVoteMsg) (*vote.RequestVoteResponse, error) {
	// stdlog.Printf("[Node %d] Received RequestVote from candidate %d for term %d",
	// 	s.nodeID, req.CandidateId, req.Term)

	raftReq := &types.RequestVoteRequest{
		Term:         int(req.Term),
		CandidateID:  string(req.CandidateId),
		LastLogIndex: int(req.LastLogIndex),
		LastLogTerm:  int(req.LastLogTerm),
	}

	raftResp, err := s.raftService.RecieveRequestVote(ctx, raftReq)
	if err != nil {
		return nil, err
	}

	// Convert from Raft domain to gRPC
	return &vote.RequestVoteResponse{
		Term:        int64(raftResp.Term),
		VoteGranted: raftResp.VoteGranted,
	}, nil
}

func (s *GRPCServer) AppendEntries(ctx context.Context, req *log.AppendEntriesMsg) (*log.AppendEntriesResponse, error) {
	// Convert proto entries to Raft entries
	//TODO: move to util
	entries := make([]types.LogEntry, len(req.Entries))

	for i, e := range req.Entries {
		entries[i] = types.LogEntry{
			Term:    int(e.Term),
			Command: e.Command,
		}
	}

	raftReq := &types.AppendEntriesRequest{
		Term:         int(req.Term),
		LeaderId:     string(req.LeaderId),
		PrevLogIndex: int(req.PrevLogIndex),
		PrevLogTerm:  int(req.PrevLogTerm),
		Entries:      entries,
		LeaderCommit: int(req.LeaderCommit),
	}

	raftResp, err := s.raftService.RecieveAppendEntries(ctx, raftReq)
	if err != nil {
		return nil, err
	}

	// Convert from Raft domain to gRPC
	return &log.AppendEntriesResponse{
		Term:    int64(raftResp.Term),
		Success: raftResp.Success,
	}, nil
}

func StartServer(addr string, nodeID string, node *raft.Node) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer lis.Close()

	grpcServer := grpc.NewServer()
	srv := NewGRPCServer(nodeID, node)
	// Register your services here
	vote.RegisterVoteServiceServer(grpcServer, srv)
	log.RegisterLogServiceServer(grpcServer, srv)

	// Handle graceful shutdown
	errChan := make(chan error, 1)
	go func() {
		stdlog.Printf("Node %d starting gRPC server on %s", nodeID, addr)
		if err := grpcServer.Serve(lis); err != nil {
			errChan <- err
		}
	}()

	// Wait for server to error out (including when killed)
	err = <-errChan
	stdlog.Printf("Node %d gRPC server on %s shutting down: %v", nodeID, addr, err)

	grpcServer.GracefulStop()
	return err
}
