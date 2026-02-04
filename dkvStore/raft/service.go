package raft

import "context"

// Service defines the interface for Raft operations
type Service interface {
	// RequestVote handles vote requests from candidates
	RequestVote(ctx context.Context, req *RequestVoteRequest) (*RequestVoteResponse, error)

	// AppendEntries handles log replication from leader
	// AppendEntries(ctx context.Context, req *AppendEntriesRequest) (*AppendEntriesResponse, error)
}

type RequestVoteRequest struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

// RequestVoteResponse represents a vote response
type RequestVoteResponse struct {
	Term        int
	VoteGranted bool
}
