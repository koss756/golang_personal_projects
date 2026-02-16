package raft

import (
	"context"
	"fmt"
	"log"
	"sync"
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

	electionTimer  time.Timer
	heartBeatTimer time.Timer

	events chan event

	resetElectionTimer  chan struct{}
	resetHeartbeatTimer chan struct{}

	transport Client
}

func NewNode(id int, peers []string, rpcClient Client) *Node {
	n := &Node{
		id:                        id,
		state:                     Follower,
		term:                      0,
		electionTimeoutLowerBound: 1500,
		electionTimeoutUpperBound: 3000,
		peers:                     peers,
		transport:                 rpcClient,
		events:                    make(chan event),
		resetElectionTimer:        make(chan struct{}, 1),
		resetHeartbeatTimer:       make(chan struct{}, 1),
	}

	n.electionTimeout = randomizedTimeout(
		n.electionTimeoutLowerBound,
		n.electionTimeoutUpperBound,
	)

	n.electionTimer = *time.NewTimer(time.Duration(n.electionTimeout) * time.Millisecond)
	go n.runElectionTimer()
	go n.runHeartBeatTimer()

	n.votesNeeded = (len(peers) / 2) + 1
	log.Printf("voted needed: %d", n.votesNeeded)
	return n
}

func (n *Node) Start() {

	for {
		ev := <-n.events
		n.handleEvent(ev)
	}
}

func (n *Node) handleElectionTimeout() {
	log.Printf("[Node %d] starting election", n.id)
	votes := make([]types.RequestVoteResponse, 0)
	if n.state != Leader {
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
		n.BroadCastVote(req, &votes)

		if len(votes)+1 > n.votesNeeded {
			log.Printf("%v These are votes", votes)
			n.Elect()
		}
	}
}

func (n *Node) State() NodeState {
	return n.state
}

func (n *Node) GetPeers() []string {
	return n.peers
}

// use pointer to votes to modify slice directly
func (n *Node) BroadCastVote(req *types.RequestVoteRequest, votes *[]types.RequestVoteResponse) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, port := range n.peers {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			resp, err := n.transport.RequestVote(ctx, port, req)
			if err != nil {
				return
			}
			log.Printf("GOT VOTE: %t", resp.VoteGranted)
			if resp.VoteGranted == true {
				mu.Lock()
				*votes = append(*votes, *resp)
				mu.Unlock()
			}
		}(port)
	}
	wg.Wait()
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
		go func(p string) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			_, err := n.transport.AppendEntries(ctx, p, req)
			if err != nil {
				log.Printf("Failed to send heartbeat to %s: %s", p, err)
			}
		}(port)

	}

}

func (n *Node) Elect() {
	log.Printf("[Node %d] Was elected As Leader", n.id)
	n.updateState(Leader)

	select {
	case n.resetElectionTimer <- struct{}{}:
	case n.resetHeartbeatTimer <- struct{}{}:
	default:
		// Channel full, timer will be reset soon anyway
	}
	// n.BroadCastEntries()
}

func (n *Node) HeartBeat() *types.LogEntry {
	return &types.LogEntry{Term: 0, Command: nil}
}

func (n *Node) RecieveRequestVote(ctx context.Context, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	respChan := make(chan *types.RequestVoteResponse)

	n.events <- requestVoteEvent{req: req, resp: respChan}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (n *Node) RecieveAppendEntries(ctx context.Context, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	respChan := make(chan *types.AppendEntriesResponse)

	n.events <- appendEntriesEvent{req: req, resp: respChan}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (n *Node) runElectionTimer() {
	timeout := randomizedTimeout(
		n.electionTimeoutLowerBound,
		n.electionTimeoutUpperBound,
	)
	timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)

	for {
		select {
		case <-timer.C:
			// Timer fired
			if n.state != Leader {
				n.events <- electionTimeout{}
				log.Printf("timeout: %d", timeout)
			}
			// Create new random timeout and reset
			timeout = randomizedTimeout(
				n.electionTimeoutLowerBound,
				n.electionTimeoutUpperBound,
			)
			timer.Reset(time.Duration(timeout) * time.Millisecond)

		case <-n.resetElectionTimer:
			// Stop the timer and drain if necessary

			log.Printf("RESET: %d", timeout)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			// Create new random timeout and reset
			timeout = randomizedTimeout(
				n.electionTimeoutLowerBound,
				n.electionTimeoutUpperBound,
			)
			timer.Reset(time.Duration(timeout) * time.Millisecond)
		}
	}
}

func (n *Node) runHeartBeatTimer() {
	timeout := 1000
	timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)

	for {
		select {
		case <-timer.C:
			if n.state == Leader {
				n.events <- heartbeatTimeout{}
			}
			timer.Reset(time.Duration(timeout) * time.Millisecond)

		case <-n.resetHeartbeatTimer:
			// Stop the timer and drain if necessary
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(time.Duration(timeout) * time.Millisecond)
		}
	}
}

func (n *Node) handleRequestVote(req *types.RequestVoteRequest) *types.RequestVoteResponse {
	if req.Term < n.term {
		return &types.RequestVoteResponse{
			Term:        n.term,
			VoteGranted: false,
		}
	}

	// if req.Term > n.term {
	//     n.becomeFollower(req.Term)
	// }

	granted := false
	if n.votedFor == 0 || n.votedFor == req.CandidateID {
		granted = true
		n.votedFor = req.CandidateID
	}

	return &types.RequestVoteResponse{
		Term:        n.term,
		VoteGranted: granted,
	}
}

func (n *Node) handleAppendEntries(req *types.AppendEntriesRequest) *types.AppendEntriesResponse {
	select {
	case n.resetElectionTimer <- struct{}{}:

	default:
		// Channel full, timer will be reset soon anyway
	}
	return &types.AppendEntriesResponse{
		Term:    n.term,
		Success: true,
	}
}

func (n *Node) handleHeartbeatTimeout() {
	if n.state == Leader {
		n.BroadCastEntries()
	}
}

func (n *Node) handleEvent(ev event) {
	switch e := ev.(type) {

	case electionTimeout:
		log.Printf("[State: %s] Election timeout event", n.state)
		n.handleElectionTimeout()

	case heartbeatTimeout:
		log.Printf("[State: %s] Heartbeat timeout event", n.state)
		n.handleHeartbeatTimeout()

	case requestVoteEvent:
		resp := n.handleRequestVote(e.req)
		log.Printf("[State %s] request vote event: %t ", n.state, resp.VoteGranted)
		e.resp <- resp

	case appendEntriesEvent:
		log.Printf("[Node %s] append entries event", n.state)
		resp := n.handleAppendEntries(e.req)
		e.resp <- resp
	}
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
