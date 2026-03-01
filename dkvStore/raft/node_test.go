package raft

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/koss756/dkvStore/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockTransport struct {
	mock.Mock
}

func (m *MockTransport) RequestVote(ctx context.Context, peer string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	args := m.Called(ctx, peer, req)
	return args.Get(0).(*types.RequestVoteResponse), args.Error(1)
}

func (m *MockTransport) AppendEntries(ctx context.Context, peer string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	args := m.Called(ctx, peer, req)
	return args.Get(0).(*types.AppendEntriesResponse), args.Error(1)
}

// Test Helpers
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

func newTestNode(id string, peers []string, transport Client) *Node {
	conf := Config{
		ElectionTimeoutLowerBound: 150,
		ElectionTimeoutUpperBound: 300,
		HeartbeatTimeout:          50,
	}

	electionTimeout := randomizedTimeout(conf.ElectionTimeoutLowerBound, conf.ElectionTimeoutUpperBound)

	n := &Node{
		id:             id,
		state:          Follower,
		term:           0,
		peers:          peers,
		transport:      transport,
		events:         make(chan event, 10),
		config:         conf,
		electionTimer:  time.NewTimer(time.Duration(electionTimeout) * time.Millisecond),
		heartbeatTimer: time.NewTimer(time.Duration(conf.HeartbeatTimeout) * time.Millisecond),
	}

	if !n.heartbeatTimer.Stop() {
		<-n.heartbeatTimer.C
	}
	// Also stop election timer so it doesn't fire during tests
	if !n.electionTimer.Stop() {
		<-n.electionTimer.C
	}

	n.votesNeeded = (len(peers) / 2) + 1
	return n
}
func TestElectionTimeout_WinsElection_BecomesLeader(t *testing.T) {
	transport := new(MockTransport)
	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(&types.RequestVoteResponse{Term: 1, VoteGranted: true}, nil)

	node := newTestNode("9001", []string{"peer2", "peer3"}, transport)
	node.handleElectionTimeout()

	assert.Equal(t, Leader, node.state)
	assert.Equal(t, 1, node.term)
	transport.AssertExpectations(t)
}

func TestElectionTimeout_TermIncrements(t *testing.T) {
	transport := new(MockTransport)
	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(&types.RequestVoteResponse{Term: 3, VoteGranted: false}, nil)

	node := newTestNode(":9000", []string{"peer2", "peer3", "peer4", "peer5"}, transport)
	node.term = 2
	node.handleElectionTimeout()

	assert.Equal(t, 3, node.term)
	transport.AssertExpectations(t)
}

func TestHandleRequestVote_GrantsVote(t *testing.T) {
	transport := new(MockTransport)

	node := newTestNode("9002", []string{"peer1"}, transport)
	node.term = 1
	node.votedFor = ""
	node.logs = []types.LogEntry{}

	req := &types.RequestVoteRequest{
		Term:         1,
		CandidateID:  "9001",
		LastLogTerm:  0,
		LastLogIndex: -1,
	}

	resp := node.handleRequestVote(req)

	assert.True(t, resp.VoteGranted)
	assert.Equal(t, 1, node.term)
	assert.Equal(t, "9001", node.votedFor)
}

func TestHandleRequestVote_AlreadyVOtedInTerm(t *testing.T) {
	transport := new(MockTransport)

	node := newTestNode("9002", []string{"peer1"}, transport)
	node.term = 1
	node.votedFor = "9002" // Some random id other than req
	node.logs = []types.LogEntry{}

	req := &types.RequestVoteRequest{
		Term:         1,
		CandidateID:  "9001",
		LastLogTerm:  0,
		LastLogIndex: 0,
	}

	resp := node.handleRequestVote(req)

	assert.False(t, resp.VoteGranted)
}

func TestHandleRequestVote_OutOfDateTerm(t *testing.T) {
	transport := new(MockTransport)

	node := newTestNode("9002", []string{"peer1"}, transport)
	node.term = 1
	node.votedFor = ""
	node.logs = []types.LogEntry{}

	req := &types.RequestVoteRequest{
		Term:         node.term + 5,
		CandidateID:  "9001",
		LastLogTerm:  0,
		LastLogIndex: -1,
	}

	node.handleRequestVote(req)

	assert.Equal(t, Follower, node.state)
	assert.Equal(t, req.Term, node.term)
}

func TestHandleRequestVote_GrantVote_WithLogs(t *testing.T) {
	transport := new(MockTransport)

	node := newTestNode("9002", []string{"peer1"}, transport)
	node.term = 1
	node.votedFor = ""
	node.logs = makeSequentialLogs(5, 1)

	fmt.Printf("Logs: %v", node.logs)

	req := &types.RequestVoteRequest{
		Term:         5,
		CandidateID:  "9001",
		LastLogTerm:  5,
		LastLogIndex: 5,
	}

	resp := node.handleRequestVote(req)

	assert.True(t, resp.VoteGranted)
}

func TestHandleRequestVote_VoteDenied_RecieverHasMorelogs(t *testing.T) {
	transport := new(MockTransport)

	node := newTestNode("9002", []string{"peer1"}, transport)
	node.term = 1
	node.votedFor = ""
	node.logs = makeSequentialLogs(5, 1)

	fmt.Printf("Logs: %v", node.logs)

	req := &types.RequestVoteRequest{
		Term:         5,
		CandidateID:  "9001",
		LastLogTerm:  5,
		LastLogIndex: 3,
	}

	resp := node.handleRequestVote(req)

	assert.False(t, resp.VoteGranted)
}

func TestHandleRequestVote_VoteDenied_RecieverHasHigherTerm(t *testing.T) {
	transport := new(MockTransport)

	node := newTestNode("9002", []string{"peer1"}, transport)
	node.term = 6
	node.votedFor = ""
	node.logs = makeSequentialLogs(5, 1)

	fmt.Printf("Logs: %v", node.logs)

	req := &types.RequestVoteRequest{
		Term:         5,
		CandidateID:  "9001",
		LastLogTerm:  5,
		LastLogIndex: 3,
	}

	resp := node.handleRequestVote(req)

	assert.False(t, resp.VoteGranted)
}
