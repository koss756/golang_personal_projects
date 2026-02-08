package raft

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/koss756/dkvStore/types"
)

type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

var stateName = map[NodeState]string{
	Follower:  "follower",
	Candidate: "candidate",
	Leader:    "leader",
}

func (state NodeState) String() string {
	return stateName[state]
}

type command struct {
	msg string
}

type logEntry struct {
	cmmd command
	term int
}

var _ Server = (*Node)(nil)

type Node struct {
	id          int
	state       NodeState
	term        int
	votedFor    int
	logs        []logEntry
	votesNeeded int
	peers       []string

	// index of highest log entry known to be committed (initialized to 0, increases monotonically)
	commitIndex int

	// index of highest log entry applied to state machine (initialized to 0, increases monotonically)
	lastApplied int

	// Leader states

	// for each server, index of the next log entry to send to that server (initialized to leader last log index + 1)
	nextIndex []int

	// for each server, index of highest log entry known to be replicated on server (initialized to 0, increases monotonically)
	matchIndex []int

	electionTimeoutLowerBound int
	electionTimeoutUpperBound int
	electionTimeout           int

	resetElectionTimer chan struct{}

	transport Client
}

func NewNode(id int, peers []string, rpcClient Client) *Node {
	n := &Node{
		id:                        id,
		state:                     Follower,
		term:                      0,
		electionTimeoutLowerBound: 150,
		electionTimeoutUpperBound: 300,
		peers:                     peers,
		transport:                 rpcClient,
		resetElectionTimer:        make(chan struct{}, 1),
	}
	n.electionTimeout = randomizedTimeout(
		n.electionTimeoutLowerBound,
		n.electionTimeoutUpperBound,
	)

	n.votesNeeded = len(peers) / 2
	return n
}

func (n *Node) Start() {
	go n.startElection()
	go n.SendHeartBeats()
}

func (n *Node) startElection() {
	electionTicker := time.NewTicker(time.Duration(n.electionTimeout) * time.Millisecond)
	defer electionTicker.Stop()

	for {
		select {
		case <-electionTicker.C:
			if n.state != Leader {
				n.BroadCastVote()
			}
		case <-n.resetElectionTimer:
			electionTicker.Reset(time.Duration(n.electionTimeout) * time.Millisecond)
		}
	}
}

func (n *Node) SendHeartBeats() NodeState {
	heartbeatTicker := time.NewTicker(100 * time.Millisecond)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-heartbeatTicker.C:
			if n.state == Leader {
				n.BroadCastEntries()
			}
		}
	}
}

func (n *Node) State() NodeState {
	return n.state
}

func (n *Node) GetPeers() []string {
	return n.peers
}

func (n *Node) BroadCastVote() {
	votes := make([]types.RequestVoteResponse, len(n.peers))

	n.state = Candidate
	n.term++
	n.votedFor = n.id
	currentTerm := n.term

	lastLogIndex := len(n.logs) - 1
	lastLogTerm := 0

	req := &types.RequestVoteRequest{
		Term:         currentTerm,
		CandidateID:  n.id,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	for _, port := range n.peers {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		resp, err := n.transport.RequestVote(ctx, port, req)

		if err != nil {
			return
		}

		if resp.VoteGranted {
			votes = append(votes, *resp)
		}
	}

	if len(votes) > n.votesNeeded {
		n.Elect()
	}
}

func (n *Node) BroadCastEntries() {
	req := &types.AppendEntriesRequest{
		Term:         n.term,
		LeaderId:     n.id,
		PrevLogIndex: 1,
		PrevLogTerm:  1,
		Entries:      []*types.LogEntry{n.HeartBeat()},
	}

	for _, port := range n.peers {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		resp, err := n.transport.AppendEntries(ctx, port, req)
		if err != nil {
			log.Printf("%s", err)
			return
		}

		log.Printf("%t", resp.Success)
	}

}

func (n *Node) Elect() {
	log.Printf("[Node %d] Was elected As Leader", n.id)
	n.updateState(Leader)
	select {
	case n.resetElectionTimer <- struct{}{}:
	default:
		// Channel full, timer will be reset soon anyway
	}
	n.BroadCastEntries()
}

func (n *Node) HeartBeat() *types.LogEntry {
	return &types.LogEntry{Term: 0, Command: nil}
}

func (n *Node) RecieveRequestVote(ctx context.Context, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	return &types.RequestVoteResponse{
		Term:        n.term,
		VoteGranted: n.GrantVote(req),
	}, nil
}

func (n *Node) GrantVote(req *types.RequestVoteRequest) bool {
	if req.Term < n.term {
		return false
	}

	// todo check log term
	if n.votedFor == 0 || n.votedFor == req.CandidateID {
		return true
	}

	return false
}

func (n *Node) RecieveAppendEntries(ctx context.Context, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	resp := &types.AppendEntriesResponse{
		Term:    n.term,
		Success: true,
	}
	log.Printf("Recieved Entries")
	select {
	case n.resetElectionTimer <- struct{}{}:
	default:
		// Channel full, timer will be reset soon anyway
	}

	// If candidate's term is outdated, reject
	if req.Term < n.term {
		return resp, nil
	}

	return resp, nil
}

func (n *Node) updateState(state NodeState) {
	switch state {
	case Follower:
		n.state = Follower
	case Candidate:
		n.state = Candidate
	case Leader:
		n.state = Leader
	default:
		panic(fmt.Errorf("unknown state: %s", state))
	}
}

func (n *Node) GetId() int {
	return n.id
}
