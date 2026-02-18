package raft

import "github.com/koss756/dkvStore/types"

type event interface{}

type electionTimeout struct{}
type heartbeatTimeout struct{}

type requestVoteEvent struct {
	req  *types.RequestVoteRequest
	resp chan *types.RequestVoteResponse
}
type appendEntriesEvent struct {
	req  *types.AppendEntriesRequest
	resp chan *types.AppendEntriesResponse
}

type commandEvent struct {
	cmd Command
}
