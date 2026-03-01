package raft

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/koss756/dkvStore/types"
)

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
	id          string // port number
	state       NodeState
	term        int
	votedFor    string
	leaderId    string
	logs        []types.LogEntry
	votesNeeded int

	peers []string // array of ports

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

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer
	timerMu        sync.Mutex

	events chan event

	// resetElectionTimer  chan struct{}
	// resetHeartbeatTimer chan struct{}
	config    Config
	transport Client
}

func NewNode(id string, peers []string, rpcClient Client, conf Config) *Node {
	electionTimeout := randomizedTimeout(conf.ElectionTimeoutLowerBound, conf.ElectionTimeoutUpperBound)

	n := &Node{
		id:             id,
		state:          Follower,
		term:           0,
		peers:          peers,
		transport:      rpcClient,
		events:         make(chan event),
		electionTimer:  time.NewTimer(time.Duration(electionTimeout) * time.Millisecond),
		heartbeatTimer: time.NewTimer(time.Duration(conf.HeartbeatTimeout) * time.Millisecond),
		config:         conf,
	}

	go n.runElectionTimer()
	go n.runHeartbeatTimer()

	n.votesNeeded = (len(peers) / 2) + 1
	return n
}

func (n *Node) Start() {
	log.Printf("[Node %s] Started", n.id)
	for {
		ev := <-n.events
		n.handleEvent(ev)
	}
}

func (n *Node) SubmitCommand(ctx context.Context, cmd []byte) error {
	// If not leader, return error or redirect
	if n.state != Leader {
		return fmt.Errorf("%s", n.leaderId)
	}

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

		log.Printf("[Node %s] Voted %t", n.id, resp.VoteGranted)

		if n.state == Candidate && resp.Term == n.term && resp.VoteGranted {
			mu.Lock()
			*votes = append(*votes, *resp)
			mu.Unlock()
		} else if resp.Term > n.term {
			n.term = resp.Term
			n.updateState(Follower)
			n.votedFor = ""
			n.resetElectionTimer()
		}
	})
}

// BroadcastEntries sends AppendEntries RPCs to all peers and returns the number of acknowledgements (including self).
// func (n *Node) BroadcastEntries(isHeartbeat bool) int {
// 	var acceptedCount int32 = 1 // count self
// 	var wg sync.WaitGroup

// 	for _, peer := range n.peers {
// 		wg.Add(1)
// 		go func(p string) {
// 			defer wg.Done()
// 			if n.replicateToPeer(p, isHeartbeat) {
// 				atomic.AddInt32(&acceptedCount, 1)
// 			}
// 		}(peer)
// 	}

// 	wg.Wait()
// 	return int(acceptedCount)
// }

// replicateToPeer handles the retry loop for a single peer, returning true if the peer acknowledged.
func (n *Node) replicateToPeer(peer string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	const timeout = 500 * time.Millisecond

	for {

		if len(req.Entries) > 0 {
			log.Printf("To peer %s with req: %+v", peer, req)
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		resp, err := n.transport.AppendEntries(ctx, peer, req)

		cancel()
		return resp, err
	}
}

func (n *Node) resetElectionTimer() {
	timeout := randomizedTimeout(n.config.ElectionTimeoutLowerBound, n.config.ElectionTimeoutUpperBound)
	n.resetTimer(n.electionTimer, time.Duration(timeout)*time.Millisecond)
}

func (n *Node) resetHeartbeatTimer() {
	n.resetTimer(n.heartbeatTimer, time.Duration(n.config.HeartbeatTimeout)*time.Millisecond)
}

func (n *Node) resetTimer(t *time.Timer, duration time.Duration) {
	n.timerMu.Lock()
	defer n.timerMu.Unlock()
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(duration)
}

func (n *Node) Elect() {
	n.updateState(Leader)

	n.nextIndex = make(map[string]int)
	n.matchIndex = make(map[string]int)

	for _, peer := range n.peers {
		n.nextIndex[peer] = len(n.logs) // sentLength
		n.matchIndex[peer] = 0          //ackedLength
	}
	n.resetElectionTimer()
	n.resetHeartbeatTimer()
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
	for {
		<-n.electionTimer.C

		n.mu.Lock()
		state := n.state
		n.mu.Unlock()

		if state != Leader {
			n.events <- electionTimeout{}
		}
		n.resetElectionTimer()
	}
}

// This timer only needs to run if there is a leader waste of CPU cycles
func (n *Node) runHeartbeatTimer() {
	for {
		<-n.heartbeatTimer.C

		n.mu.Lock()
		state := n.state
		n.mu.Unlock()

		if state == Leader {
			n.events <- heartbeatTimeout{}
		}
		n.resetHeartbeatTimer()
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
			n.votedFor = ""
			n.resetElectionTimer()
		}
	}
}

func (n *Node) handleHeartbeatTimeout() {
	if n.state == Leader {
		for _, peer := range n.peers {
			req := n.replicateLog(peer, true)
			resp, err := n.replicateToPeer(peer, req)

			if err != nil {
				log.Printf("Heartbeat was denied")
			}

			log.Printf("Heartbeat response %+v", resp)
		}
	}
}

func (n *Node) handleRequestVote(req *types.RequestVoteRequest) *types.RequestVoteResponse {
	// Our Log is out of date revert to follower
	if req.Term > n.term {
		n.term = req.Term
		n.updateState(Follower)
		n.votedFor = ""
	}

	lastTerm := 0
	if len(n.logs) > 0 {
		lastTerm = n.logs[len(n.logs)-1].Term
	}

	// verify candidate log is not out of date
	logOk := (req.LastLogTerm > lastTerm) || (req.LastLogTerm == lastTerm && req.LastLogIndex >= len(n.logs)-1)

	if req.Term == n.term && logOk && (n.votedFor == req.CandidateID || n.votedFor == "") {
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

func (n *Node) handleAppendEntries(req *types.AppendEntriesRequest) *types.AppendEntriesResponse {
	if req.Term > n.term {
		n.term = req.Term
		n.updateState(Follower)
		n.votedFor = ""
		n.leaderId = req.LeaderId

		log.Printf("WE HIT?")
	}

	logOk := (len(n.logs) >= req.PrevLogIndex) && (req.PrevLogIndex == 0 || n.logs[req.PrevLogIndex-1].Term == req.PrevLogTerm)

	if req.Term == n.term && logOk {
		n.appendEntries(req.PrevLogIndex, req.LeaderCommit, req.Entries)
		ack := req.PrevLogIndex + len(req.Entries)
		log.Printf("[Node %s] Accepted append entries %+v", n.id, req)

		n.resetElectionTimer()
		return &types.AppendEntriesResponse{
			FollowerId: n.id,
			Term:       n.term,
			Ack:        ack,
			Success:    true,
		}
	} else {
		log.Printf("[Node %s] Denied append entries %+v", n.id, req)
		return &types.AppendEntriesResponse{
			FollowerId: n.id,
			Term:       n.term,
			Ack:        0,
			Success:    false,
		}
	}
}

func (n *Node) appendEntries(prevLogIndex int, leaderCommit int, entries []types.LogEntry) {
	if len(entries) > 0 && len(n.logs) > prevLogIndex {
		index := min(len(n.logs), prevLogIndex+len(entries)) - 1

		if n.logs[index].Term != entries[index-prevLogIndex].Term {
			n.logs = n.logs[:prevLogIndex]
		}
	}

	if prevLogIndex+len(entries) > len(n.logs) {
		for i := len(n.logs) - prevLogIndex; i <= len(entries)-1; i++ {
			n.logs = append(n.logs, entries[i])
		}
	}

	if leaderCommit > n.commitIndex {
		for i := n.commitIndex; i < leaderCommit; i++ {
			// deliver n.logs[i].cmd to app
			log.Printf("Command delivered")
		}
		n.commitIndex = leaderCommit
	}
}

func (n *Node) handleCommand(cmd []byte) {
	logEntry := types.LogEntry{
		Command: cmd,
		Term:    n.term,
	}

	n.logs = append(n.logs, logEntry)
	// n.matchIndex[n.id] := len(n.logs)

	for _, peer := range n.peers {
		req := n.replicateLog(peer, false)
		n.replicateToPeer(peer, req)
	}

	// acceptedCount := n.BroadcastEntries(false)

	// if acceptedCount > len(n.peers)/2+1 {
	// 	go func() { n.events <- commitLogEvent{cmd: cmd} }()
	// }
}

func (n *Node) replicateLog(peer string, isHeartBeat bool) *types.AppendEntriesRequest {
	prevLogIndex := n.nextIndex[peer] // index of the next log entry to send to follower

	var entries []types.LogEntry

	if !isHeartBeat {
		entries = n.logs[prevLogIndex:] // log entries to store
	}

	prevLogTerm := 0
	if prevLogIndex > 0 {
		prevLogTerm = n.logs[prevLogIndex-1].Term
	}

	return &types.AppendEntriesRequest{
		Term:         n.term,
		LeaderId:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
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

func (n *Node) GetId() string {
	return n.id
}

// func (n *Node) hasMatchingLog(prevLogIndex, prevLogTerm int) bool {
// 	if prevLogIndex < 0 {
// 		return true // no previous entry to check
// 	}

// 	if prevLogIndex >= len(n.logs) {
// 		return true
// 	}

// 	if n.logs[prevLogIndex].Term != prevLogTerm {
// 		log.Printf("Terms do not match node: %d, Leader: %d", n.logs[prevLogIndex].Term, prevLogTerm)
// 		return false
// 	}

// 	return true
// }

// func (n *Node) hasConflictedLogs(req *types.AppendEntriesRequest) {
// 	for i, entry := range req.Entries {
// 		logIndex := req.PrevLogIndex + i + 1 // 1-based

// 		if logIndex <= len(n.logs) {
// 			if n.logs[logIndex-1].Term != entry.Term {
// 				// Conflict: truncate everything from here and append the rest
// 				n.logs = n.logs[:logIndex-1]
// 				n.logs = append(n.logs, *entry)
// 				n.logs = append(n.logs, toValueSlice(req.Entries[i+1:])...)
// 				break
// 			}
// 			// Already matches, skip
// 		} else {
// 			// Past end of our log, just append remaining
// 			n.logs = append(n.logs, toValueSlice(req.Entries[i:])...)
// 			break
// 		}
// 	}
// }

// buildAppendEntriesRequest constructs the RPC request for the given peer.
// func (n *Node) buildAppendEntriesRequest(peer string, isHeartbeat bool) (*types.AppendEntriesRequest, int) {
// 	prevIndex := n.nextIndex[peer] - 1
// 	if prevIndex < 0 {
// 		prevIndex = 0
// 	}

// 	var prevLogTerm int
// 	if len(n.logs) > 0 && prevIndex < len(n.logs) {
// 		prevLogTerm = n.logs[prevIndex].Term
// 	}

// 	var entries []types.LogEntry

// 	if !isHeartbeat {
// 		logSlice := n.logs[n.nextIndex[peer]:]
// 		entries = make([]types.LogEntry, len(logSlice))
// 	}

// 	return &types.AppendEntriesRequest{
// 		Term:         n.term,
// 		LeaderId:     n.id,
// 		PrevLogIndex: prevIndex,
// 		PrevLogTerm:  prevLogTerm,
// 		Entries:      entries,
// 	}, prevIndex
// }
