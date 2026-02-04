package api

import (
	"context"
	"log"
	"net"

	"github.com/koss756/dkvStore/api/vote"
	"github.com/koss756/dkvStore/raft"
	"google.golang.org/grpc"
)

type GRPCServer struct {
	vote.UnimplementedVoteServiceServer
	raftService raft.Service
}

func NewGRPCServer(raftService raft.Service) *GRPCServer {
	return &GRPCServer{raftService: raftService}
}

func (s *GRPCServer) RequestVote(ctx context.Context, req *vote.RequestVoteMsg) (*vote.RequestVoteMsg, error) {
	raftReq := &raft.RequestVoteRequest{
		Term:         int(req.Term),
		CandidateID:  int(req.CandidateId),
		LastLogIndex: int(req.LastLogIndex),
		LastLogTerm:  int(req.LastLogTerm),
	}

	raftResp, err := s.raftService.RequestVote(ctx, raftReq)
	if err != nil {
		return nil, err
	}

	// Convert from Raft domain to gRPC
	return &vote.RequestVoteMsg{
		Term: int64(raftResp.Term),
		// VoteGranted: raftResp.VoteGranted,
	}, nil
}

func StartServer(port string, raftService raft.Service) error {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer()
	voteServer := NewGRPCServer(raftService)

	vote.RegisterVoteServiceServer(grpcServer, voteServer)

	log.Printf("Starting gRPC server on %s", port)
	return grpcServer.Serve(lis)
}
