package raft

import (
	"context"
	"fmt"
	"time"
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

var _ RPCService = (*Node)(nil)

type Node struct {
	id       int
	state    NodeState
	term     int
	votedFor int
	logs     []logEntry

	peers []string

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

	transport Service
}

func NewNode(peers []string, rpcClient Service) *Node {
	n := &Node{
		state:                     Follower,
		term:                      0,
		electionTimeoutLowerBound: 150,
		electionTimeoutUpperBound: 300,
		peers:                     peers,
		transport:                 rpcClient,
	}
	n.electionTimeout = randomizedTimeout(
		n.electionTimeoutLowerBound,
		n.electionTimeoutUpperBound,
	)
	return n
}

func (n *Node) OtherStart() {

}

func (n *Node) Start() {
	fmt.Println("Start election")
	go n.startElection()
}

func (n *Node) startElection() {
	n.state = Candidate
	n.term++
	n.votedFor = n.id
	currentTerm := n.term

	lastLogIndex := len(n.logs) - 1
	lastLogTerm := 0

	// Vote for self
	// votesReceived := 1
	// votesNeeded := (len(n.peers)+1)/2 + 1

	req := &RequestVoteRequest{
		Term:         currentTerm,
		CandidateID:  n.id,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	n.BroadCastVote(req)
}

func (n *Node) State() NodeState {
	return n.state
}

func (n *Node) BroadCastVote(req *RequestVoteRequest) {
	for _, port := range n.peers {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		resp, err := n.transport.RequestVote(ctx, port, req)

		if err != nil {
			return
		}

		fmt.Println(resp.Term)

	}
}

func (n *Node) ElectionTimeout() int {
	return n.electionTimeout
}

func (n *Node) RequestVote(ctx context.Context, req *RequestVoteRequest) (*RequestVoteResponse, error) {
	resp := &RequestVoteResponse{
		Term:        n.term,
		VoteGranted: false,
	}

	// If candidate's term is outdated, reject
	if req.Term < n.term {
		return resp, nil
	}

	return resp, nil
}

func (n *Node) onElectionTimeout() {
	electionTimer := time.NewTimer(time.Duration(n.electionTimeout))
	<-electionTimer.C

	fmt.Println("Timer fired")

	// wait for election timeout period

	// if timeout run election
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
