package raft

import (
	"context"

	"github.com/koss756/dkvStore/types"
)

type Client interface {
	RequestVote(ctx context.Context, target string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error)
	AppendEntries(ctx context.Context, target string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error)
}

type Server interface {
	RecieveRequestVote(ctx context.Context, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error)
	RecieveAppendEntries(ctx context.Context, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error)
}
