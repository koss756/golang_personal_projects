package grpc

import (
	"context"
	"fmt"

	"github.com/koss756/dkvStore/api/grpc/log"
	"github.com/koss756/dkvStore/api/grpc/vote"
	"github.com/koss756/dkvStore/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RaftClient struct {
	connections map[string]*grpc.ClientConn
	voteClients map[string]vote.VoteServiceClient
	logClients  map[string]log.LogServiceClient
}

func NewGRPCClient(peerAddresses []string) (*RaftClient, error) {
	voteClients := make(map[string]vote.VoteServiceClient)
	logClients := make(map[string]log.LogServiceClient)
	connections := make(map[string]*grpc.ClientConn)

	for _, addr := range peerAddresses {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("failed to connect to %s: %v", addr, err)
		}

		voteClients[addr] = vote.NewVoteServiceClient(conn)
		logClients[addr] = log.NewLogServiceClient(conn)
		connections[addr] = conn
	}

	return &RaftClient{
		voteClients: voteClients,
		logClients:  logClients,
		connections: connections,
	}, nil
}

func (c *RaftClient) Close() error {
	for _, conn := range c.connections {
		if err := conn.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (c *RaftClient) RequestVote(ctx context.Context, target string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	// Get the specific connection for this target
	client, ok := c.voteClients[target]
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

	return &types.RequestVoteResponse{
		Term:        int(grpcResp.Term),
		VoteGranted: bool(grpcResp.VoteGranted),
	}, nil
}

func (c *RaftClient) AppendEntries(ctx context.Context, target string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	// Get the specific connection for this target
	client, ok := c.logClients[target]
	if !ok {
		return nil, fmt.Errorf("no connection to %s", target)
	}

	entries := make([]*log.LogEntry, 0, len(req.Entries))

	for _, e := range req.Entries {
		entries = append(entries, &log.LogEntry{
			Term:    int64(e.Term),
			Command: e.Command,
		})
	}

	grpcReq := &log.AppendEntriesMsg{
		Term:         int64(req.Term),
		LeaderId:     int64(req.LeaderId),
		PrevLogIndex: int64(req.PrevLogIndex),
		PrevLogTerm:  int64(req.PrevLogTerm),
		Entries:      entries,
		LeaderCommit: int64(req.LeaderCommit),
	}

	grpcResp, err := client.AppendEntries(ctx, grpcReq)
	if err != nil {
		return nil, err
	}

	return &types.AppendEntriesResponse{
		Term:    int(grpcResp.Term),
		Success: true,
	}, nil
}
