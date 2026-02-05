package api

import (
	"context"
	"fmt"

	"github.com/koss756/dkvStore/api/vote"
	"github.com/koss756/dkvStore/raft"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPC_Client struct {
	conn        *grpc.ClientConn
	connections map[string]vote.VoteServiceClient
}

func NewGRPCClient(peerAddresses []string) (*GRPC_Client, error) {
	connections := make(map[string]vote.VoteServiceClient)

	for _, addr := range peerAddresses {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("failed to connect to %s: %v", addr, err)
		}

		connections[addr] = vote.NewVoteServiceClient(conn)
	}

	return &GRPC_Client{connections: connections}, nil
}

func (c *GRPC_Client) Close() error {
	return c.conn.Close()
}

func (c *GRPC_Client) RequestVote(ctx context.Context, target string, req *raft.RequestVoteRequest) (*raft.RequestVoteResponse, error) {
	// Get the specific connection for this target
	client, ok := c.connections[target]
	if !ok {
		return nil, fmt.Errorf("no connection to %s", target)
	}

	grpcReq := &vote.RequestVoteMsg{
		Term:         int64(req.Term),
		CandidateId:  int64(req.CandidateID),
		LastLogIndex: int64(req.LastLogIndex),
		LastLogTerm:  int64(req.LastLogTerm),
	}

	grpcResp, err := client.RequestVote(ctx, grpcReq)
	if err != nil {
		return nil, err
	}

	return &raft.RequestVoteResponse{
		Term:        int(grpcResp.Term),
		VoteGranted: true,
	}, nil
}
