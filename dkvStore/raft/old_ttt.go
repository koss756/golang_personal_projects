package raft

// import (
// 	"context"
// 	"fmt"
// 	"testing"
// 	"time"

// 	"github.com/koss756/dkvStore/types"
// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/mock"
// )

// // Test Helpers
// func makeLogs(n int, term int) []types.LogEntry {
// 	logs := make([]types.LogEntry, n)
// 	for i := 0; i < n; i++ {
// 		logs[i] = types.LogEntry{
// 			Term:    term,
// 			Command: []byte{},
// 		}
// 	}
// 	return logs
// }

// func makeSequentialLogs(n int, startTerm int) []types.LogEntry {
// 	logs := make([]types.LogEntry, n)
// 	for i := 0; i < n; i++ {
// 		logs[i] = types.LogEntry{
// 			Term:    startTerm + i,
// 			Command: []byte(fmt.Sprintf("cmd-%d", i)),
// 		}
// 	}
// 	return logs
// }

// func newTestNode(id string, peers []string, transport Client) *Node {
// 	conf := Config{
// 		ElectionTimeoutLowerBound: 150,
// 		ElectionTimeoutUpperBound: 300,
// 		HeartbeatTimeout:          50,
// 	}

// 	electionTimeout := randomizedTimeout(conf.ElectionTimeoutLowerBound, conf.ElectionTimeoutUpperBound)

// 	n := &Node{
// 		id:             id,
// 		state:          Follower,
// 		term:           0,
// 		peers:          peers,
// 		transport:      transport,
// 		events:         make(chan event, 10),
// 		config:         conf,
// 		electionTimer:  time.NewTimer(time.Duration(electionTimeout) * time.Millisecond),
// 		heartbeatTimer: time.NewTimer(time.Duration(conf.HeartbeatTimeout) * time.Millisecond),
// 	}

// 	if !n.heartbeatTimer.Stop() {
// 		<-n.heartbeatTimer.C
// 	}
// 	// Also stop election timer so it doesn't fire during tests
// 	if !n.electionTimer.Stop() {
// 		<-n.electionTimer.C
// 	}

// 	n.votesNeeded = (len(peers) / 2) + 1
// 	return n
// }
// func TestElectionTimeout_WinsElection_BecomesLeader(t *testing.T) {
// 	transport := new(MockTransport)
// 	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
// 		Return(&types.RequestVoteResponse{Term: 1, VoteGranted: true}, nil)

// 	node := newTestNode("9001", []string{"peer2", "peer3"}, transport)
// 	node.handleElectionTimeout()

// 	assert.Equal(t, Leader, node.state)
// 	assert.Equal(t, 1, node.term)
// 	transport.AssertExpectations(t)
// }

// func TestElectionTimeout_TermIncrements(t *testing.T) {
// 	transport := new(MockTransport)
// 	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
// 		Return(&types.RequestVoteResponse{Term: 3, VoteGranted: false}, nil)

// 	node := newTestNode(":9000", []string{"peer2", "peer3", "peer4", "peer5"}, transport)
// 	node.term = 2
// 	node.handleElectionTimeout()

// 	assert.Equal(t, 3, node.term)
// 	transport.AssertExpectations(t)
// }

// // REQUEST VOTE
// func TestHandleRequestVote_GrantsVote(t *testing.T) {
// 	transport := new(MockTransport)

// 	node := newTestNode("9002", []string{"peer1"}, transport)
// 	node.term = 1
// 	node.votedFor = ""
// 	node.logs = []types.LogEntry{}

// 	req := &types.RequestVoteRequest{
// 		Term:         1,
// 		CandidateID:  "9001",
// 		LastLogTerm:  0,
// 		LastLogIndex: -1,
// 	}

// 	resp := node.handleRequestVote(req)

// 	assert.True(t, resp.VoteGranted)
// 	assert.Equal(t, 1, node.term)
// 	assert.Equal(t, "9001", node.votedFor)
// }

// func TestHandleRequestVote_AlreadyVOtedInTerm(t *testing.T) {
// 	transport := new(MockTransport)

// 	node := newTestNode("9002", []string{"peer1"}, transport)
// 	node.term = 1
// 	node.votedFor = "9002" // Some random id other than req
// 	node.logs = []types.LogEntry{}

// 	req := &types.RequestVoteRequest{
// 		Term:         1,
// 		CandidateID:  "9001",
// 		LastLogTerm:  0,
// 		LastLogIndex: 0,
// 	}

// 	resp := node.handleRequestVote(req)

// 	assert.False(t, resp.VoteGranted)
// }

// func TestHandleRequestVote_OutOfDateTerm(t *testing.T) {
// 	transport := new(MockTransport)

// 	node := newTestNode("9002", []string{"peer1"}, transport)
// 	node.term = 1
// 	node.votedFor = ""
// 	node.logs = []types.LogEntry{}

// 	req := &types.RequestVoteRequest{
// 		Term:         node.term + 5,
// 		CandidateID:  "9001",
// 		LastLogTerm:  0,
// 		LastLogIndex: -1,
// 	}

// 	node.handleRequestVote(req)

// 	assert.Equal(t, Follower, node.state)
// 	assert.Equal(t, req.Term, node.term)
// }

// func TestHandleRequestVote_GrantVote_WithLogs(t *testing.T) {
// 	transport := new(MockTransport)

// 	node := newTestNode("9002", []string{"peer1"}, transport)
// 	node.term = 1
// 	node.votedFor = ""
// 	node.logs = makeSequentialLogs(5, 1)

// 	fmt.Printf("Logs: %v", node.logs)

// 	req := &types.RequestVoteRequest{
// 		Term:         5,
// 		CandidateID:  "9001",
// 		LastLogTerm:  5,
// 		LastLogIndex: 5,
// 	}

// 	resp := node.handleRequestVote(req)

// 	assert.True(t, resp.VoteGranted)
// }

// func TestHandleRequestVote_VoteDenied_RecieverHasMorelogs(t *testing.T) {
// 	transport := new(MockTransport)

// 	node := newTestNode("9002", []string{"peer1"}, transport)
// 	node.term = 1
// 	node.votedFor = ""
// 	node.logs = makeSequentialLogs(5, 1)

// 	fmt.Printf("Logs: %v", node.logs)

// 	req := &types.RequestVoteRequest{
// 		Term:         5,
// 		CandidateID:  "9001",
// 		LastLogTerm:  5,
// 		LastLogIndex: 3,
// 	}

// 	resp := node.handleRequestVote(req)

// 	assert.False(t, resp.VoteGranted)
// }

// func TestHandleRequestVote_VoteDenied_RecieverHasHigherTerm(t *testing.T) {
// 	transport := new(MockTransport)

// 	node := newTestNode("9002", []string{"peer1"}, transport)
// 	node.term = 6
// 	node.votedFor = ""
// 	node.logs = makeSequentialLogs(5, 1)

// 	fmt.Printf("Logs: %v", node.logs)

// 	req := &types.RequestVoteRequest{
// 		Term:         5,
// 		CandidateID:  "9001",
// 		LastLogTerm:  5,
// 		LastLogIndex: 3,
// 	}

// 	resp := node.handleRequestVote(req)

// 	assert.False(t, resp.VoteGranted)
// }

// // APPEND ENTRIES
// func TestHandleAppendEntries_HigherTermBecomesFollower(t *testing.T) {
// 	transport := new(MockTransport)
// 	node := newTestNode("9000", []string{"peer1"}, transport)

// 	node.state = Leader
// 	node.term = 1
// 	node.votedFor = "self"
// 	node.logs = makeSequentialLogs(3, 1)

// 	req := &types.AppendEntriesRequest{
// 		Term:         2,
// 		LeaderId:     "9001",
// 		PrevLogIndex: 0,
// 		PrevLogTerm:  1,
// 		Entries:      makeLogs(1, 2),
// 		LeaderCommit: 0,
// 	}

// 	resp := node.handleAppendEntries(req)

// 	assert.Equal(t, Follower, node.state)
// 	assert.Equal(t, 2, node.term)
// 	assert.Equal(t, "", node.votedFor)
// 	assert.Equal(t, "9001", node.leaderId)
// 	assert.True(t, resp.Success)
// }

// func TestHandleAppendEntries_LowerTermRejected(t *testing.T) {
// 	transport := new(MockTransport)
// 	node := newTestNode("9000", []string{"peer1"}, transport)

// 	node.term = 5
// 	node.logs = makeSequentialLogs(3, 1)

// 	req := &types.AppendEntriesRequest{
// 		Term:         3,
// 		LeaderId:     "9001",
// 		PrevLogIndex: 1,
// 		PrevLogTerm:  2,
// 		Entries:      makeLogs(1, 3),
// 		LeaderCommit: 0,
// 	}

// 	resp := node.handleAppendEntries(req)

// 	assert.False(t, resp.Success)
// 	assert.Equal(t, 5, resp.Term)
// 	assert.Equal(t, 0, resp.Ack)
// }

// func TestHandleAppendEntries_ValidAppend(t *testing.T) {
// 	transport := new(MockTransport)
// 	node := newTestNode("9000", []string{"peer1"}, transport)

// 	// Terms: [1,2,3]
// 	node.term = 3
// 	node.logs = makeSequentialLogs(3, 1)

// 	req := &types.AppendEntriesRequest{
// 		Term:         3,
// 		LeaderId:     "9001",
// 		PrevLogIndex: 2,
// 		PrevLogTerm:  3,
// 		Entries:      makeLogs(2, 3),
// 		LeaderCommit: 0,
// 	}

// 	resp := node.handleAppendEntries(req)

// 	assert.True(t, resp.Success)
// 	assert.Equal(t, 4, resp.Ack) // 2 + 2
// 	assert.Equal(t, 5, len(node.logs))
// }

// func TestHandleAppendEntries_PrevLogIndexTooLarge(t *testing.T) {
// 	transport := new(MockTransport)
// 	node := newTestNode("9000", []string{"peer1"}, transport)

// 	node.term = 2
// 	node.logs = makeSequentialLogs(2, 1)

// 	req := &types.AppendEntriesRequest{
// 		Term:         2,
// 		LeaderId:     "9001",
// 		PrevLogIndex: 5, // invalid
// 		PrevLogTerm:  1,
// 		Entries:      makeLogs(1, 2),
// 		LeaderCommit: 0,
// 	}

// 	resp := node.handleAppendEntries(req)

// 	assert.False(t, resp.Success)
// 	assert.Equal(t, 0, resp.Ack)
// 	assert.Equal(t, 2, len(node.logs))
// }

// func TestHandleAppendEntries_PrevLogTermMismatch(t *testing.T) {
// 	transport := new(MockTransport)
// 	node := newTestNode("9000", []string{"peer1"}, transport)

// 	// Terms: [1,2,3]
// 	node.term = 3
// 	node.logs = makeSequentialLogs(3, 1)

// 	req := &types.AppendEntriesRequest{
// 		Term:         3,
// 		LeaderId:     "9001",
// 		PrevLogIndex: 1, // term should be 2
// 		PrevLogTerm:  99,
// 		Entries:      makeLogs(1, 3),
// 		LeaderCommit: 0,
// 	}

// 	resp := node.handleAppendEntries(req)

// 	assert.False(t, resp.Success)
// 	assert.Equal(t, 0, resp.Ack)
// 	assert.Equal(t, 3, len(node.logs))
// }

// func TestHandleAppendEntries_Heartbeat(t *testing.T) {
// 	transport := new(MockTransport)
// 	node := newTestNode("9000", []string{"peer1"}, transport)

// 	node.term = 3
// 	node.logs = makeSequentialLogs(3, 1)

// 	req := &types.AppendEntriesRequest{
// 		Term:         3,
// 		LeaderId:     "9001",
// 		PrevLogIndex: 2,
// 		PrevLogTerm:  3,
// 		Entries:      []types.LogEntry{},
// 		LeaderCommit: 0,
// 	}

// 	resp := node.handleAppendEntries(req)

// 	assert.True(t, resp.Success)
// 	assert.Equal(t, 2, resp.Ack)
// 	assert.Equal(t, 3, len(node.logs))
// }
