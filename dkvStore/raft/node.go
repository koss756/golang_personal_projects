package raft

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/koss756/dkvStore/kvstore"
	"github.com/koss756/dkvStore/types"
)

var _ Server = (*Node)(nil)
var _ CommandHandler = (*Node)(nil)

type Config struct {
	ElectionTimeoutLowerBound int
	ElectionTimeoutUpperBound int
	HeartbeatTimeout          int
	HTTPAddr                  string
	ID                        string
	GRPCAddr                  string
	Peers                     []Peer
}

type Peer struct {
	ID       string
	GRPCAddr string
}

type Node struct {
	mu            sync.RWMutex
	state         NodeState
	term          int
	votedFor      string
	leaderId      string
	logs          []types.LogEntry
	votesReceived int
	votesNeeded   int

	peers []Peer

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

	store *kvstore.KVStore
}

func NewNode(rpcClient Client, conf Config) *Node {
	electionTimeout := randomizedTimeout(conf.ElectionTimeoutLowerBound, conf.ElectionTimeoutUpperBound)

	n := &Node{
		state:          Follower,
		term:           0,
		peers:          conf.Peers,
		transport:      rpcClient,
		votesReceived:  0,
		events:         make(chan event),
		electionTimer:  time.NewTimer(time.Duration(electionTimeout) * time.Millisecond),
		heartbeatTimer: time.NewTimer(time.Duration(conf.HeartbeatTimeout) * time.Millisecond),
		config:         conf,
		store:          kvstore.NewKVStore(),
	}

	go n.runElectionTimer()
	go n.runHeartbeatTimer()

	n.votesNeeded = (len(conf.Peers) / 2) + 1
	return n
}

func (n *Node) Start() {
	log.Printf("[Node %s] Started", n.config.ID)
	for {
		ev := <-n.events
		n.handleEvent(ev)
	}
}

// recives the command from http server
func (n *Node) SubmitCommand(ctx context.Context, cmd []byte) error {
	// If not leader, return error or redirect
	if n.state != Leader {
		return fmt.Errorf("%s", n.config.HTTPAddr)
	}

	n.events <- commandEvent{cmd: cmd}
	return nil
}

// replicateToPeer handles the retry loop for a single peer, returning true if the peer acknowledged.
func (n *Node) replicateToPeer(peer string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	const timeout = 500 * time.Millisecond

	if len(req.Entries) > 0 {
		log.Printf("To peer %s with req: %+v", peer, req)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := n.transport.AppendEntries(ctx, peer, req)

	return resp, err
}

func (n *Node) peerAddr(peerID string) (string, bool) {
	for _, p := range n.peers {
		if p.ID == peerID {
			return p.GRPCAddr, true
		}
	}
	return "", false
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
	n.state = Leader

	n.nextIndex = make(map[string]int)
	n.matchIndex = make(map[string]int)

	for _, peer := range n.peers {
		// the index of the log entry to send to that follower
		n.nextIndex[peer.ID] = len(n.logs) // sentLength

		// the index of the highest known log entry committed by the follower
		n.matchIndex[peer.ID] = 0 //ackedLength
	}
	n.matchIndex[n.config.ID] = len(n.logs)

	n.resetElectionTimer()
	n.resetHeartbeatTimer()
}

func (n *Node) HeartBeat() *types.LogEntry {
	return &types.LogEntry{Term: 0, Command: nil}
}

func (n *Node) RecieveRequestVote(ctx context.Context, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	respChan := make(chan *types.RequestVoteResponse, 1)

	n.events <- requestVoteEvent{req: req, resp: respChan}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Follower Recieves Append Entries from Leader.
// Adds appendEntries Events event to be handled. reads from channel and reuturns the response
func (n *Node) RecieveAppendEntries(ctx context.Context, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
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

func (n *Node) runHeartbeatTimer() {
	for {
		<-n.heartbeatTimer.C

		state := n.state

		if state == Leader {
			n.events <- heartbeatTimeout{}
		}
		n.resetHeartbeatTimer()
	}
}

func (n *Node) handleEvent(ev event) {
	switch e := ev.(type) {

	case electionTimeout:
		n.handleElectionTimeout()
	case heartbeatTimeout:
		n.handleHeartbeatTimeout()
	case requestVoteEvent:
		resp := n.handleRequestVote(e.req)
		e.resp <- resp
	case appendEntriesEvent:
		resp := n.handleAppendEntries(e.req)
		e.resp <- resp
	case commandEvent:
		n.handleCommand(e.cmd)
	case commitLogEvent:
		log.Printf("WE COMMIT THE LOG!")
	case appendEntriesResponseEvent:
		n.handleAppendEntriesResponse(e.peer, e.resp)
	case voteResponseEvent:
		n.handleVoteResponse(e.peer, e.resp)
	}
}

func (n *Node) handleVoteResponse(peer string, resp *types.RequestVoteResponse) {
	// If we learn about a higher term, step down even if we're not a candidate anymore.
	if resp.Term > n.term {
		n.term = resp.Term
		n.state = Follower
		n.votedFor = ""
		return
	}

	// Ignore stale responses (or responses irrelevant to our current candidacy).
	if n.state != Candidate || resp.Term != n.term {
		return
	}

	if resp.VoteGranted {
		n.votesReceived++ // new field on Node, reset to 1 (self) at election start
	}

	if n.votesReceived >= n.votesNeeded {
		n.Elect()
	}
}

func (n *Node) handleElectionTimeout() {
	if n.state == Leader {
		return
	}

	n.state = Candidate
	n.term++
	n.votedFor = n.config.ID
	currentTerm := n.term

	lastLogIndex := len(n.logs) - 1
	lastLogTerm := 0

	if len(n.logs) > 0 {
		lastLogTerm = n.logs[lastLogIndex].Term
	}

	req := &types.RequestVoteRequest{
		Term:         currentTerm,
		CandidateID:  n.config.ID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	// vote for self
	n.votesReceived += 1

	for _, peer := range n.peers {
		go func(p string) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			resp, err := n.transport.RequestVote(ctx, p, req)
			if err != nil {
				// handle bad resp?
				return
			}
			n.events <- voteResponseEvent{peer: p, resp: resp}
		}(peer.GRPCAddr)
	}
}

func (n *Node) handleHeartbeatTimeout() {
	if n.state == Leader {
		for _, peer := range n.peers {
			req := n.replicateLog(peer.ID, true)
			go func(peerID string, peerAddr string, r *types.AppendEntriesRequest) {
				resp, err := n.replicateToPeer(peerAddr, r)
				if err != nil {
					return
				}
				// track replication by peer ID, but dial by peer address
				n.events <- appendEntriesResponseEvent{peer: peerID, resp: resp}
			}(peer.ID, peer.GRPCAddr, req)
		}
	}
}

// Nodes receive rpc from potential leader
func (n *Node) handleRequestVote(req *types.RequestVoteRequest) *types.RequestVoteResponse {
	// Our Log is out of date revert to follower
	if req.Term > n.term {
		n.term = req.Term
		n.state = Follower
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

// Followers recieving messages
func (n *Node) handleAppendEntries(req *types.AppendEntriesRequest) *types.AppendEntriesResponse {
	if req.Term > n.term {
		n.term = req.Term
		n.votedFor = ""
		n.resetElectionTimer()
	}

	if req.Term == n.term {
		n.state = Follower
		n.leaderId = req.LeaderId
	}

	logOk := (len(n.logs) >= req.PrevLogIndex) && (req.PrevLogIndex == 0 || n.logs[req.PrevLogIndex-1].Term == req.PrevLogTerm)

	if req.Term == n.term && logOk {
		n.appendEntries(req.PrevLogIndex, req.LeaderCommit, req.Entries)
		// Replicated length (not PrevLogIndex+len(Entries): heartbeats would report 0).
		ack := len(n.logs)
		n.resetElectionTimer()
		return &types.AppendEntriesResponse{
			FollowerId: n.config.ID,
			Term:       n.term,
			Ack:        ack,
			Success:    true,
		}
	} else {
		return &types.AppendEntriesResponse{
			FollowerId: n.config.ID,
			Term:       n.term,
			Ack:        0,
			Success:    false,
		}
	}
}

func (n *Node) appendEntries(prevLogIndex int, leaderCommit int, entries []types.LogEntry) {
	log.Printf("[Node %s] AppendEntries start | prevLogIndex=%d leaderCommit=%d entries=%d localLogLen=%d commitIndex=%d",
		n.config.ID, prevLogIndex, leaderCommit, len(entries), len(n.logs), n.commitIndex,
	)

	if len(entries) > 0 && len(n.logs) > prevLogIndex {
		index := min(len(n.logs), prevLogIndex+len(entries)) - 1

		log.Printf("[Node %s] Checking conflict at index=%d (localTerm=%d, incomingTerm=%d)",
			n.config.ID,
			index,
			n.logs[index].Term,
			entries[index-prevLogIndex].Term,
		)

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
			if err := n.store.Apply(n.logs[i].Command); err != nil {
				log.Printf("[Node %s] StateMachine.Apply failed at index=%d: %v", n.config.ID, i, err)
			}

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
	n.matchIndex[n.config.ID] = len(n.logs)

	for _, peer := range n.peers {
		req := n.replicateLog(peer.ID, false)
		go n.sendAppendEntries(peer.ID, peer.GRPCAddr, req)
	}
}

func (n *Node) sendAppendEntries(peerID string, peerAddr string, req *types.AppendEntriesRequest) {
	resp, err := n.replicateToPeer(peerAddr, req)
	if err != nil {
		return
	}

	n.events <- appendEntriesResponseEvent{
		peer: peerID,
		resp: resp,
	}
}

func (n *Node) handleAppendEntriesResponse(peer string, resp *types.AppendEntriesResponse) {
	if resp.Term == n.term && n.state == Leader {
		if resp.Success && resp.Ack >= n.matchIndex[peer] {
			n.nextIndex[peer] = resp.Ack
			n.matchIndex[peer] = resp.Ack
			n.commitLogEntries()
		} else if n.nextIndex[peer] > 0 {
			n.nextIndex[peer] = n.nextIndex[peer] - 1
			req := n.replicateLog(peer, false)
			if addr, ok := n.peerAddr(peer); ok {
				go n.sendAppendEntries(peer, addr, req)
			}
		}
	} else if resp.Term > n.term {
		n.term = resp.Term
		n.state = Follower
		n.votedFor = ""
		n.resetElectionTimer()
	}
}

// acks(length) = |{ n ∈ nodes | ackedLength[n] ≥ length }|
func acks(matchIndex map[string]int, nodes []string, length int) int {
	var c int
	for _, id := range nodes {
		if matchIndex[id] >= length {
			c++
		}
	}
	return c
}

// CommitLogEntries advances commitLength when a quorum has acked a prefix and the
// last entry of that prefix is from the current term. Pseudo-code uses commitLength
// as the committed prefix length; we store that in n.commitIndex.
func (n *Node) commitLogEntries() {
	if n.state != Leader || len(n.logs) == 0 {
		return
	}

	nodes := make([]string, 0, 1+len(n.peers))
	nodes = append(nodes, n.config.ID)
	for _, peer := range n.peers {
		nodes = append(nodes, peer.ID)
	}

	minAcks := len(nodes)/2 + 1

	logLen := len(n.logs)
	ready := make([]int, 0, logLen)
	for length := 1; length <= logLen; length++ {
		if acks(n.matchIndex, nodes, length) >= minAcks {
			ready = append(ready, length)
		}
	}
	if len(ready) == 0 {
		return
	}

	maxReady := ready[len(ready)-1]
	commitLength := n.commitIndex
	if maxReady <= commitLength || n.logs[maxReady-1].Term != n.term {
		return
	}

	for i := commitLength; i < maxReady; i++ {
		if err := n.store.Apply(n.logs[i].Command); err != nil {
			log.Printf("[Node %s] StateMachine.Apply failed at index=%d: %v", n.config.ID, i, err)
		}
	}
	n.commitIndex = maxReady
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
		LeaderId:     n.config.ID,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}
}

func (n *Node) GetId() string {
	return n.config.ID
}

func (n *Node) GetStore() map[string]string {
	return n.store.GetAll()
}
