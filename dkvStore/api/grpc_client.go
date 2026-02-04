package api

import (
	"context"

	"github.com/koss756/dkvStore/api/vote"
	"github.com/koss756/dkvStore/raft"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPC_Client struct {
	conn   *grpc.ClientConn
	client vote.VoteServiceClient
}

func NewGRPCClient(addr string) (*GRPC_Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		return nil, err
	}

	return &GRPC_Client{
		conn:   conn,
		client: vote.NewVoteServiceClient(conn),
	}, nil
}

func (c *GRPC_Client) Close() error {
	return c.conn.Close()
}

func (c *GRPC_Client) RequestVote(ctx context.Context, req *raft.RequestVoteRequest) (*raft.RequestVoteResponse, error) {
	grpcReq := &vote.RequestVoteMsg{
		Term:         int64(req.Term),
		CandidateId:  int64(req.CandidateID),
		LastLogIndex: int64(req.LastLogIndex),
		LastLogTerm:  int64(req.LastLogTerm),
	}

	grpcResp, err := c.client.RequestVote(ctx, grpcReq)

	if err != nil {
		return nil, err
	}

	return &raft.RequestVoteResponse{
		Term:        int(grpcResp.Term),
		VoteGranted: true,
	}, nil
}
