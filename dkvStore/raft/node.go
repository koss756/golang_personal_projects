package raft

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/koss756/dkvStore/types"
)

type Command struct {
	Op    string `json:"op"` // "set" | "delete"
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
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
	mu          sync.RWMutex
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

	//LEADER STATES

	// for each server, index of the next log entry to send to that server
	// (initialized to leader last log index + 1)
	nextIndex map[string]int

	// for each server, index of highest log entry known to be replicated on server
	// (initialized to 0)
	matchIndex map[string]int

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
	log.Printf("[Node %d] Started", n.id)
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

	log.Printf("SUBMIT COMMAND %+v", cmd)

	n.events <- commandEvent{cmd: cmd}
	return nil
}

// use pointer to votes to modify slice directly
func (n *Node) BroadCastVote(req *types.RequestVoteRequest, votes *[]types.RequestVoteResponse) {
	var mu sync.Mutex

	broadcastToPeers(500*time.Millisecond, n.peers, func(ctx context.Context, peer string) {
		resp, err := n.transport.RequestVote(ctx, peer, req)
		if err != nil {
			return
		}

		log.Printf("[Node %d] Voted %t", n.id, resp.VoteGranted)

		if n.state == Candidate && resp.Term == n.term && resp.VoteGranted {
			mu.Lock()
			*votes = append(*votes, *resp)
			mu.Unlock()
		} else if resp.Term > n.term {
			n.term = resp.Term
			n.updateState(Follower)
			n.votedFor = 0
			n.resetElectionTimer <- struct{}{}
		}
	})
}

// BroadcastEntries sends AppendEntries RPCs to all peers and returns the number of acknowledgements (including self).
func (n *Node) BroadcastEntries(isHeartbeat bool) int {
	var acceptedCount int32 = 1 // count self
	var wg sync.WaitGroup

	for _, peer := range n.peers {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if n.replicateToPeer(p, isHeartbeat) {
				atomic.AddInt32(&acceptedCount, 1)
			}
		}(peer)
	}

	wg.Wait()
	return int(acceptedCount)
}

// replicateToPeer handles the retry loop for a single peer, returning true if the peer acknowledged.
func (n *Node) replicateToPeer(peer string, isHeartbeat bool) bool {
	const timeout = 500 * time.Millisecond

	for {
		req, prevIndex := n.buildAppendEntriesRequest(peer, isHeartbeat)

		if len(req.Entries) > 0 {
			log.Printf("To peer %s with req: %+v", peer, req)
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		resp, err := n.transport.AppendEntries(ctx, peer, req)

		if len(req.Entries) > 0 {
			log.Printf("\n[Node %d] AppendEntries sent to %s with resp: %+v\n", n.id, peer, resp)
		}

		cancel()

		if err != nil {
			log.Printf("Failed to send entries to %s: %s", peer, err)
			return false
		}

		if resp.Success {
			if !isHeartbeat {
				n.nextIndex[peer] = prevIndex + len(req.Entries) + 1
				n.matchIndex[peer] = n.nextIndex[peer] - 1
			}
			return true
		}

		if isHeartbeat {
			return false
		}

		// Decrement nextIndex and retry
		if !n.decrementNextIndex(peer) {
			return false
		}
	}
}

// buildAppendEntriesRequest constructs the RPC request for the given peer.
func (n *Node) buildAppendEntriesRequest(peer string, isHeartbeat bool) (*types.AppendEntriesRequest, int) {
	prevIndex := n.nextIndex[peer] - 1
	if prevIndex < 0 {
		prevIndex = 0
	}

	var prevLogTerm int
	if len(n.logs) > 0 && prevIndex < len(n.logs) {
		prevLogTerm = n.logs[prevIndex].Term
	}

	var entries []*types.LogEntry
	if !isHeartbeat {
		logSlice := n.logs[n.nextIndex[peer]:]
		entries = make([]*types.LogEntry, len(logSlice))
		for i := range logSlice {
			entries[i] = &logSlice[i]
		}
	}

	return &types.AppendEntriesRequest{
		Term:         n.term,
		LeaderId:     n.id,
		PrevLogIndex: prevIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
	}, prevIndex
}

// decrementNextIndex steps back the nextIndex for a peer during log conflict resolution.
// Returns false if we've bottomed out and should give up.
func (n *Node) decrementNextIndex(peer string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.nextIndex[peer] > 1 {
		n.nextIndex[peer]--
		return true
	}
	return false
}

func (n *Node) Elect() {
	n.updateState(Leader)

	n.nextIndex = make(map[string]int)
	n.matchIndex = make(map[string]int)

	for _, peer := range n.peers {
		n.nextIndex[peer] = len(n.logs)
		n.matchIndex[peer] = 0
	}
	log.Printf("[Node  %d] Elected Leader", n.id)
	n.resetElectionTimer <- struct{}{}
	n.resetHeartbeatTimer <- struct{}{}
}

func (n *Node) HeartBeat() *types.LogEntry {
	return &types.LogEntry{Term: 0, Command: nil}
}

func (n *Node) RecieveRequestVote(ctx context.Context, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	// If RPC request contains term T > currentTerm: set currentTerm = T, convert to follower (§5.1)
	// n.isHigherTerm(req.Term)

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
	// n.isHigherTerm(req.Term)

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
		// log.Printf("[State: %s] Election timeout event", n.state)
		n.handleElectionTimeout()
	case heartbeatTimeout:
		// log.Printf("[State: %s] Heartbeat timeout event", n.state)
		n.handleHeartbeatTimeout()
	case requestVoteEvent:
		resp := n.handleRequestVote(e.req)
		e.resp <- resp
	case appendEntriesEvent:
		resp := n.handleAppendEntries(e.req)
		e.resp <- resp
	case commandEvent:
		log.Printf("command Event!")
		n.handleCommand(e.cmd)
	case commitLogEvent:
		log.Printf("WE COMMIT THE LOG!")
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

		if len(n.logs) > 0 {
			lastLogTerm = n.logs[lastLogIndex].Term
		}

		req := &types.RequestVoteRequest{
			Term:         currentTerm,
			CandidateID:  n.id,
			LastLogIndex: lastLogIndex,
			LastLogTerm:  lastLogTerm,
		}
		// Wait for response
		n.BroadCastVote(req, &votes)

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
		n.BroadcastEntries(true)
	}
}

func (n *Node) handleRequestVote(req *types.RequestVoteRequest) *types.RequestVoteResponse {
	// Our Log is out of date revert to follower
	if req.Term > n.term {
		n.term = req.Term
		n.updateState(Follower)
		n.votedFor = 0
	}

	lastTerm := 0
	if len(n.logs) > 0 {
		lastTerm = n.logs[len(n.logs)-1].Term
	}

	// verify candidate log is not out of date
	logOk := (req.LastLogTerm > lastTerm) || (req.LastLogTerm == lastTerm && req.LastLogIndex >= len(n.logs)-1)

	if req.Term == n.term && logOk && (n.votedFor == req.CandidateID || n.votedFor == 0) {
		n.votedFor = req.CandidateID
		return &types.RequestVoteResponse{
			Term:        n.term,
			VoteGranted: true,
		}
	} else {
		return &types.RequestVoteResponse{
			Term:        n.term,
			VoteGranted: false,
		}
	}
}

func (n *Node) hasMatchingLog(prevLogIndex, prevLogTerm int) bool {
	if prevLogIndex < 0 {
		return true // no previous entry to check
	}

	if prevLogIndex >= len(n.logs) {
		return true
	}

	if n.logs[prevLogIndex].Term != prevLogTerm {
		log.Printf("Terms do not match node: %d, Leader: %d", n.logs[prevLogIndex].Term, prevLogTerm)
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
		log.Printf("Rejected Append Entries %v", req.Entries)
		return acceptAppendEntry(false, n.term)
	}

	select {
	case n.resetElectionTimer <- struct{}{}:
	default:
	}

	// Rule 2: Reply false if log doesn't contain a matching entry at prevLogIndex
	if !n.hasMatchingLog(req.PrevLogIndex, req.PrevLogTerm) && req.PrevLogIndex != 0 {
		log.Printf("[Node %d] Rejected append entries, broke rule 2 %v, %+v", n.id, n.logs, req)
		return acceptAppendEntry(false, n.term)
	}

	// Rule 3: Walk new entries, detect conflicts, truncate and append
	n.hasConflictedLogs(req)

	// Update leaderCommit
	if req.LeaderCommit > n.commitIndex {
		lastNewIndex := req.PrevLogIndex + len(req.Entries)
		n.commitIndex = min(req.LeaderCommit, lastNewIndex)
	}

	if len(req.Entries) > 0 {
		var parts []string

		for _, entry := range req.Entries {
			cmd, err := DeserializeCommand(entry.Command)
			if err != nil {
				parts = append(parts, fmt.Sprintf("[term=%d invalid-cmd]", entry.Term))
				continue
			}

			parts = append(parts, fmt.Sprintf("[term=%d cmd=%+v]", entry.Term, cmd))
		}

		log.Printf("Accepted Append Entries: %s", strings.Join(parts, " "))
	}
	n.leaderId = req.LeaderId
	return acceptAppendEntry(true, n.term)
}

func (n *Node) handleCommand(cmd Command) {
	log.Printf("We are go this command: %+v", cmd)
	data, err := SerializeCommand(cmd)

	if err != nil {
		log.Printf("Failed to serialize command: %v", err)
	}

	entry := types.LogEntry{
		Command: data,
		Term:    n.term,
	}

	n.logs = append(n.logs, entry)
	acceptedCount := n.BroadcastEntries(false)
	if acceptedCount > len(n.peers)/2+1 {
		go func() { n.events <- commitLogEvent{cmd: cmd} }()
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
