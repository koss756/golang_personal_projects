package raft

import (
	"context"
	"testing"
	"time"

	"github.com/koss756/dkvStore/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ── handleVoteResponse unit tests ────────────────────────────────────────────

// A candidate that receives enough votes should become leader.
func TestHandleVoteResponse_QuorumReached_BecomesLeader(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.state = Candidate
	n.term = 1
	n.votedFor = n.id
	n.votesReceived = 1 // self vote

	n.handleVoteResponse("9001", &types.RequestVoteResponse{
		Term:        1,
		VoteGranted: true,
	})

	assert.Equal(t, Leader, n.state)
}

// A candidate that doesn't reach quorum should stay a candidate.
func TestHandleVoteResponse_NoQuorum_StaysCandidate(t *testing.T) {
	transport := new(MockTransport)
	// 5-node cluster needs 3 votes; self + 1 = 2, not enough
	n := newTestNode("9000", []string{"9001", "9002", "9003", "9004"}, transport)
	n.state = Candidate
	n.term = 1
	n.votedFor = n.id
	n.votesReceived = 1

	n.handleVoteResponse("9001", &types.RequestVoteResponse{
		Term:        1,
		VoteGranted: true,
	})

	assert.Equal(t, Candidate, n.state)
}

// A vote response from a stale term should be ignored entirely.
func TestHandleVoteResponse_StaleTerm_Ignored(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.state = Candidate
	n.term = 3
	n.votesReceived = 1

	n.handleVoteResponse("9001", &types.RequestVoteResponse{
		Term:        1, // stale
		VoteGranted: true,
	})

	assert.Equal(t, Candidate, n.state)
	assert.Equal(t, 1, n.votesReceived) // vote not counted
}

// A response with a higher term should revert the node to follower.
func TestHandleVoteResponse_HigherTerm_StepsDown(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.state = Candidate
	n.term = 1
	n.votesReceived = 1

	n.handleVoteResponse("9001", &types.RequestVoteResponse{
		Term:        5, // higher than ours
		VoteGranted: false,
	})

	assert.Equal(t, Follower, n.state)
	assert.Equal(t, 5, n.term)
	assert.Equal(t, "", n.votedFor)
}

// Denied votes should not increment the vote count.
func TestHandleVoteResponse_DeniedVote_NotCounted(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.state = Candidate
	n.term = 1
	n.votesReceived = 1

	n.handleVoteResponse("9001", &types.RequestVoteResponse{
		Term:        1,
		VoteGranted: false,
	})

	assert.Equal(t, Candidate, n.state)
	assert.Equal(t, 1, n.votesReceived)
}

// ── handleRequestVote unit tests ─────────────────────────────────────────────

// A node should grant a vote to a candidate with a higher term and up-to-date log.
func TestHandleRequestVote_GrantsVote(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.term = 1

	resp := n.handleRequestVote(&types.RequestVoteRequest{
		Term:         2,
		CandidateID:  "9001",
		LastLogIndex: 0,
		LastLogTerm:  0,
	})

	assert.True(t, resp.VoteGranted)
	assert.Equal(t, "9001", n.votedFor)
	assert.Equal(t, 2, n.term)
}

// A node should not grant a second vote in the same term to a different candidate.
func TestHandleRequestVote_AlreadyVoted_Denied(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.term = 1
	n.votedFor = "9001"

	resp := n.handleRequestVote(&types.RequestVoteRequest{
		Term:        1,
		CandidateID: "9002", // different candidate, same term
	})

	assert.False(t, resp.VoteGranted)
	assert.Equal(t, "9001", n.votedFor) // unchanged
}

// A node should deny a vote to a candidate with a stale term.
func TestHandleRequestVote_StaleTerm_Denied(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001"}, transport)
	n.term = 5

	resp := n.handleRequestVote(&types.RequestVoteRequest{
		Term:        3, // behind our term
		CandidateID: "9001",
	})

	assert.False(t, resp.VoteGranted)
}

// A node should deny a vote to a candidate whose log is behind ours.
func TestHandleRequestVote_CandidateLogBehind_Denied(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001"}, transport)
	n.term = 2
	n.logs = makeLogs(3, 2) // we have 3 entries at term 2

	resp := n.handleRequestVote(&types.RequestVoteRequest{
		Term:         2,
		CandidateID:  "9001",
		LastLogIndex: 1, // candidate only has 1 entry
		LastLogTerm:  2,
	})

	assert.False(t, resp.VoteGranted)
}

// A node should grant a vote to the same candidate again (idempotent).
func TestHandleRequestVote_SameCandidate_Regranted(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001"}, transport)
	n.term = 1
	n.votedFor = "9001"

	resp := n.handleRequestVote(&types.RequestVoteRequest{
		Term:        1,
		CandidateID: "9001",
	})

	assert.True(t, resp.VoteGranted)
}

// ── full election integration tests ──────────────────────────────────────────

// A node that receives votes from a quorum should elect itself leader.
func TestElection_WinsWithQuorum(t *testing.T) {
	transport := new(MockTransport)

	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(grantVote(1))

	transport.On("AppendEntries", mock.Anything, mock.Anything, mock.Anything).
		Return(acceptAppendEntries(1))

	n := newRunningTestNode("9000", []string{"9001", "9002"}, transport)
	n.events <- electionTimeout{}

	waitForState(t, n, Leader, 1*time.Second)
	assert.Equal(t, Leader, n.state)
}

// A node that is denied votes by all peers should remain a follower.
func TestElection_LosesWithNoVotes(t *testing.T) {
	transport := new(MockTransport)

	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(denyVote(1))

	n := newRunningTestNode("9000", []string{"9001", "9002"}, transport)
	n.events <- electionTimeout{}

	time.Sleep(300 * time.Millisecond)

	n.mu.RLock()
	state := n.state
	n.mu.RUnlock()

	assert.NotEqual(t, Leader, state)
}

// A node that starts an election but sees a higher term should revert to follower.
func TestElection_HigherTermResponse_StepsDown(t *testing.T) {
	transport := new(MockTransport)

	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(&types.RequestVoteResponse{Term: 10, VoteGranted: false}, nil)

	n := newRunningTestNode("9000", []string{"9001", "9002"}, transport)
	n.events <- electionTimeout{}

	waitForState(t, n, Follower, 1*time.Second)

	n.mu.RLock()
	term := n.term
	n.mu.RUnlock()

	assert.Equal(t, Follower, n.state)
	assert.Equal(t, 10, term)
}

// A candidate that receives an AppendEntries from a valid leader should
// step down to follower immediately.
func TestElection_CandidateReceivesAppendEntries_StepsDown(t *testing.T) {
	transport := new(MockTransport)
	n := newRunningTestNode("9000", []string{"9001", "9002"}, transport)
	n.state = Candidate
	n.term = 2

	// send AppendEntries through the event loop
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	resp, err := n.RecieveAppendEntries(ctx, &types.AppendEntriesRequest{
		Term:         2,
		LeaderId:     "9001",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      []types.LogEntry{},
		LeaderCommit: 0,
	})

	assert.NoError(t, err)
	assert.True(t, resp.Success)

	n.mu.RLock()
	state := n.state
	n.mu.RUnlock()
	assert.Equal(t, Follower, state)
}

// A node should not start a new election if it is already the leader.
func TestElection_LeaderIgnoresElectionTimeout(t *testing.T) {
	transport := new(MockTransport)
	transport.On("AppendEntries", mock.Anything, mock.Anything, mock.Anything).
		Return(acceptAppendEntries(1))

	n := newRunningTestNode("9000", []string{"9001", "9002"}, transport)
	n.state = Leader
	n.term = 1
	n.nextIndex = map[string]int{"9001": 0, "9002": 0}
	n.matchIndex = map[string]int{"9001": 0, "9002": 0}

	n.events <- electionTimeout{}

	time.Sleep(100 * time.Millisecond)

	n.mu.RLock()
	state := n.state
	term := n.term
	n.mu.RUnlock()

	assert.Equal(t, Leader, state)
	assert.Equal(t, 1, term)
}

// ── term/voted-for reset tests ────────────────────────────────────────────────

// After stepping down due to a higher term, votedFor should be cleared.
func TestStepDown_ClearsVotedFor(t *testing.T) {
	transport := new(MockTransport)
	n := newTestNode("9000", []string{"9001"}, transport)
	n.state = Candidate
	n.term = 1
	n.votedFor = n.id

	n.handleVoteResponse("9001", &types.RequestVoteResponse{
		Term:        5,
		VoteGranted: false,
	})

	assert.Equal(t, "", n.votedFor)
	assert.Equal(t, Follower, n.state)
	assert.Equal(t, 5, n.term)
}

// On winning election, nextIndex should be initialized for all peers.
func TestElect_InitializesLeaderState(t *testing.T) {
	transport := new(MockTransport)
	transport.On("AppendEntries", mock.Anything, mock.Anything, mock.Anything).
		Return(acceptAppendEntries(1))

	n := newTestNode("9000", []string{"9001", "9002"}, transport)
	n.term = 1
	n.logs = makeLogs(3, 1)

	n.Elect()

	assert.Equal(t, Leader, n.state)
	assert.Equal(t, 3, n.nextIndex["9001"])
	assert.Equal(t, 3, n.nextIndex["9002"])
	assert.Equal(t, 0, n.matchIndex["9001"])
	assert.Equal(t, 0, n.matchIndex["9002"])
}

// ── RequestVote RPC receiver tests ───────────────────────────────────────────
// RecieveRequestVote should route through the event loop and return correctly.
func TestRecieveRequestVote_RoutedThroughEventLoop(t *testing.T) {
	transport := new(MockTransport)
	n := newRunningTestNode("9000", []string{"9001"}, transport)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	resp, err := n.RecieveRequestVote(ctx, &types.RequestVoteRequest{
		Term:        1,
		CandidateID: "9001",
	})

	assert.NoError(t, err)
	assert.True(t, resp.VoteGranted)
}

// RecieveRequestVote should respect context cancellation.
func TestRecieveRequestVote_ContextCancelled(t *testing.T) {
	transport := new(MockTransport)
	// do NOT start event loop — nothing will consume the event
	n := newTestNode("9000", []string{"9001"}, transport)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := n.RecieveRequestVote(ctx, &types.RequestVoteRequest{
		Term:        1,
		CandidateID: "9001",
	})

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
