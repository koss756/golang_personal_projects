package raft

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/koss756/dkvStore/types"
	"github.com/stretchr/testify/mock"
)

// ── MockTransport ─────────────────────────────────────────────────────────────

type MockTransport struct {
	mock.Mock
}

func (m *MockTransport) RequestVote(ctx context.Context, peer string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	args := m.Called(ctx, peer, req)
	resp, _ := args.Get(0).(*types.RequestVoteResponse)
	return resp, args.Error(1)
}

func (m *MockTransport) AppendEntries(ctx context.Context, peer string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	args := m.Called(ctx, peer, req)
	resp, _ := args.Get(0).(*types.AppendEntriesResponse)
	return resp, args.Error(1)
}

// ── Node constructors ─────────────────────────────────────────────────────────

// newTestNode creates a node with timers stopped and no event loop running.
// Use for unit tests that call handlers directly.
func newTestNode(id string, peers []string, transport Client) *Node {
	peerList := make([]Peer, 0, len(peers))
	for _, p := range peers {
		// Tests often use a single string to represent a peer; treat it as both ID and address.
		peerList = append(peerList, Peer{ID: p, GRPCAddr: p})
	}

	conf := Config{
		ElectionTimeoutLowerBound: 150,
		ElectionTimeoutUpperBound: 300,
		HeartbeatTimeout:          50,
		GRPCAddr:                  ":9000",
		HTTPAddr:                  ":8000",
		ID:                        id,
		Peers:                     peerList,
	}

	electionTimeout := randomizedTimeout(conf.ElectionTimeoutLowerBound, conf.ElectionTimeoutUpperBound)

	n := &Node{
		state:          Follower,
		term:           0,
		peers:          peerList,
		transport:      transport,
		events:         make(chan event, 10),
		config:         conf,
		electionTimer:  time.NewTimer(time.Duration(electionTimeout) * time.Millisecond),
		heartbeatTimer: time.NewTimer(time.Duration(conf.HeartbeatTimeout) * time.Millisecond),
		lastApplied:    0,
		commitIndex:    0,
	}

	if !n.heartbeatTimer.Stop() {
		<-n.heartbeatTimer.C
	}
	if !n.electionTimer.Stop() {
		<-n.electionTimer.C
	}

	total := 1 + len(peers)
	n.votesNeeded = total/2 + 1
	return n
}

// newRunningTestNode creates a node with the event loop running.
// Use for integration tests that send events and wait for state changes.
func newRunningTestNode(id string, peers []string, transport Client) *Node {
	n := newTestNode(id, peers, transport)
	go n.Start()
	return n
}

// ── Waiting helpers ───────────────────────────────────────────────────────────

func waitForState(t *testing.T, n *Node, state NodeState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n.mu.RLock()
		current := n.state
		n.mu.RUnlock()
		if current == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	n.mu.RLock()
	current := n.state
	n.mu.RUnlock()
	t.Errorf("timed out waiting for state %s, got %s", state, current)
}

// ── Log helpers ───────────────────────────────────────────────────────────────

func makeLogs(n int, term int) []types.LogEntry {
	logs := make([]types.LogEntry, n)
	for i := 0; i < n; i++ {
		logs[i] = types.LogEntry{
			Term:    term,
			Command: []byte{},
		}
	}
	return logs
}

func makeSequentialLogs(n int, startTerm int) []types.LogEntry {
	logs := make([]types.LogEntry, n)
	for i := 0; i < n; i++ {
		logs[i] = types.LogEntry{
			Term:    startTerm + i,
			Command: []byte(fmt.Sprintf("cmd-%d", i)),
		}
	}
	return logs
}

// ── Response helpers ──────────────────────────────────────────────────────────

func grantVote(term int) (*types.RequestVoteResponse, error) {
	return &types.RequestVoteResponse{Term: term, VoteGranted: true}, nil
}

func denyVote(term int) (*types.RequestVoteResponse, error) {
	return &types.RequestVoteResponse{Term: term, VoteGranted: false}, nil
}

func acceptAppendEntries(term int) (*types.AppendEntriesResponse, error) {
	return &types.AppendEntriesResponse{Term: term, Success: true}, nil
}
