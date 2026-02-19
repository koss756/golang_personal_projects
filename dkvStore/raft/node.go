package raft

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/koss756/dkvStore/types"
)

type Command struct {
	Key   string
	Value string
}

var _ Server = (*Node)(nil)
var _ CommandHandler = (*Node)(nil)

type Config struct {
	ElectionTimeoutLowerBound int
	ElectionTimeoutUpperBound int
	HeartbeatTimeout          int
	httpAddr                  string
}

type Node struct {
	id          int
	state       NodeState
	term        int
	votedFor    int
	leaderId    int
	logs        []types.LogEntry
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

	electionTimer  time.Timer
	heartBeatTimer time.Timer

	events chan event

	resetElectionTimer  chan struct{}
	resetHeartbeatTimer chan struct{}
	config              Config
	transport           Client
}

func NewNode(id int, peers []string, rpcClient Client, conf Config) *Node {
	n := &Node{
		id:                  id,
		state:               Follower,
		term:                0,
		peers:               peers,
		transport:           rpcClient,
		events:              make(chan event),
		resetElectionTimer:  make(chan struct{}, 1),
		resetHeartbeatTimer: make(chan struct{}, 1),
		config:              conf,
	}

	go n.runElectionTimer()
	go n.runHeartBeatTimer()

	n.votesNeeded = (len(peers) / 2) + 1
	return n
}

func (n *Node) Start() {
	for {
		ev := <-n.events
		n.handleEvent(ev)
	}
}

func (n *Node) SubmitCommand(ctx context.Context, cmd Command) error {
	// If not leader, return error or redirect
	if n.state != Leader {
		return fmt.Errorf("%d", n.leaderId)
	}

	n.events <- commandEvent{cmd: cmd}
	return nil
}

// use pointer to votes to modify slice directly
func (n *Node) BroadCastVote(req *types.RequestVoteRequest, votes *[]types.RequestVoteResponse) {
	var mu sync.Mutex

	broadcastToPeers(100*time.Millisecond, n.peers, func(ctx context.Context, peer string) {
		resp, err := n.transport.RequestVote(ctx, peer, req)
		if err != nil {
			return
		}

		if resp.VoteGranted {
			mu.Lock()
			*votes = append(*votes, *resp)
			mu.Unlock()
		}
	})
}

func (n *Node) BroadCastEntries(logEntry *types.LogEntry) {
	var entries []*types.LogEntry

	if logEntry != nil {
		entries = []*types.LogEntry{logEntry}
	} else {
		entries = nil // heartbeat
	}

	req := &types.AppendEntriesRequest{
		Term:         n.term,
		LeaderId:     n.id,
		PrevLogIndex: 1,
		PrevLogTerm:  1,
		Entries:      entries,
	}

	broadcastToPeers(100*time.Millisecond, n.peers, func(ctx context.Context, peer string) {
		_, err := n.transport.AppendEntries(ctx, peer, req)
		if err != nil {
			log.Printf("Failed to send heartbeat to %s: %s", peer, err)
		}
	})
}

func (n *Node) Elect() {
	n.updateState(Leader)
	n.resetElectionTimer <- struct{}{}
	n.resetHeartbeatTimer <- struct{}{}
}

func (n *Node) HeartBeat() *types.LogEntry {
	return &types.LogEntry{Term: 0, Command: nil}
}

func (n *Node) RecieveRequestVote(ctx context.Context, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	// If RPC request contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
	n.isHigherTerm(req.Term)

	respChan := make(chan *types.RequestVoteResponse, 1)

	n.events <- requestVoteEvent{req: req, resp: respChan}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (n *Node) RecieveAppendEntries(ctx context.Context, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	// If RPC request contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
	n.isHigherTerm(req.Term)

	respChan := make(chan *types.AppendEntriesResponse, 1)

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
		n.config.ElectionTimeoutLowerBound,
		n.config.ElectionTimeoutUpperBound,
	)
	timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)

	for {
		select {
		case <-timer.C:
			// Timer fired
			if n.state != Leader {
				n.events <- electionTimeout{}
			}
			// Create new random timeout and reset
			timeout = randomizedTimeout(
				n.config.ElectionTimeoutLowerBound,
				n.config.ElectionTimeoutUpperBound,
			)
			timer.Reset(time.Duration(timeout) * time.Millisecond)

		case <-n.resetElectionTimer:
			// Stop the timer and drain if necessary
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			// Create new random timeout and reset
			timeout = randomizedTimeout(
				n.config.ElectionTimeoutLowerBound,
				n.config.ElectionTimeoutUpperBound,
			)
			timer.Reset(time.Duration(timeout) * time.Millisecond)
		}
	}
}

// This timer only needs to run if there is a leader waste of CPU cycles
func (n *Node) runHeartBeatTimer() {
	timeout := n.config.HeartbeatTimeout
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
		resp := n.handleAppendEntries(e.req)
		e.resp <- resp
	case commandEvent:
		log.Printf("command Event!")
		n.handleCommand(e.cmd)
	}
}

func (n *Node) handleElectionTimeout() {
	votes := make([]types.RequestVoteResponse, 0)

	if n.state != Leader {
		n.updateState(Candidate)
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
		// Wait for response
		n.BroadCastVote(req, &votes)

		log.Printf("Votes: %v", votes)

		// vote for self so add 1
		if len(votes)+1 > n.votesNeeded {
			n.Elect()
		} else {
			n.state = Follower
			n.votedFor = 0
			n.resetElectionTimer <- struct{}{}
		}
	}
}

func (n *Node) handleHeartbeatTimeout() {
	if n.state == Leader {
		n.BroadCastEntries(nil)
	}
}

func (n *Node) isHigherTerm(term int) bool {
	if term > n.term {
		n.term = term
		n.updateState(Follower)
	}
	return term > n.term
}

func (n *Node) handleRequestVote(req *types.RequestVoteRequest) *types.RequestVoteResponse {
	if req.Term < n.term {
		return &types.RequestVoteResponse{
			Term:        n.term,
			VoteGranted: false,
		}
	}

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

func (n *Node) hasMatchingLog(prevLogIndex, prevLogTerm int) bool {
	if prevLogIndex == 0 {
		return true
	}

	if prevLogIndex > len(n.logs) {
		return false
	}

	// check if the logs index match
	if n.logs[prevLogIndex].Term != prevLogTerm {
		return false
	}

	return true
}

func (n *Node) hasConflictedLogs(req *types.AppendEntriesRequest) {
	for i, entry := range req.Entries {
		logIndex := req.PrevLogIndex + i + 1 // 1-based

		if logIndex <= len(n.logs) {
			if n.logs[logIndex-1].Term != entry.Term {
				// Conflict: truncate everything from here and append the rest
				n.logs = n.logs[:logIndex-1]
				n.logs = append(n.logs, *entry)
				n.logs = append(n.logs, toValueSlice(req.Entries[i+1:])...)
				break
			}
			// Already matches, skip
		} else {
			// Past end of our log, just append remaining
			n.logs = append(n.logs, toValueSlice(req.Entries[i:])...)
			break
		}
	}
}

func (n *Node) handleAppendEntries(req *types.AppendEntriesRequest) *types.AppendEntriesResponse {
	if req.Term < n.term {
		return acceptAppendEntry(false, n.term)
	}

	select {
	case n.resetElectionTimer <- struct{}{}:
	default:
	}

	// Rule 2: Reply false if log doesn't contain a matching entry at prevLogIndex
	if !n.hasMatchingLog(req.PrevLogIndex, req.PrevLogTerm) {
		return acceptAppendEntry(false, n.term)
	}

	// Rule 3: Walk new entries, detect conflicts, truncate and append
	n.hasConflictedLogs(req)

	// Update leaderCommit
	if req.LeaderCommit > n.commitIndex {
		lastNewIndex := req.PrevLogIndex + len(req.Entries)
		n.commitIndex = min(req.LeaderCommit, lastNewIndex)
	}

	n.leaderId = req.LeaderId
	return acceptAppendEntry(true, n.term)
}

func (n *Node) handleCommand(cmd Command) {
	data, err := SerializeCommand(cmd)

	if err != nil {
		log.Printf("Failed to serialize command: %v", err)
	}

	entry := types.LogEntry{
		Command: data,
		Term:    n.term,
	}

	// leader appends to log entry
	n.logs = append(n.logs, entry)
	n.BroadCastEntries(&entry)
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
