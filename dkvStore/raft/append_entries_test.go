package raft

import (
	"testing"
	"time"

	// "time"

	"github.com/koss756/dkvStore/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ── Recieve Command tests ───────────────────────────────────────────

// TestHandleCommand_AppendToLog verifies that handleCommand appends the entry
// to the leader's log with the correct term and command.
func TestHandleCommand_AppendToLog(t *testing.T) {
	transport := &MockTransport{}
	transport.On("AppendEntries", mock.Anything, mock.Anything, mock.Anything).
		Return(acceptAppendEntries(1))

	n := newTestNode("node1", []string{"node2", "node3"}, transport)
	n.state = Leader
	n.term = 1
	n.logs = []types.LogEntry{}
	n.matchIndex = map[string]int{n.config.ID: 0}
	n.nextIndex = map[string]int{"node2": 1, "node3": 1}

	cmd := []byte("set x=1")
	n.handleCommand(cmd)

	assert.Len(t, n.logs, 1)
	assert.Equal(t, cmd, n.logs[0].Command)
	assert.Equal(t, 1, n.logs[0].Term)
}

// TestHandleCommand_UpdatesMatchIndex verifies that after appending,
// the leader's own matchIndex reflects the new log length.
func TestHandleCommand_UpdatesMatchIndex(t *testing.T) {
	transport := &MockTransport{}
	transport.On("AppendEntries", mock.Anything, mock.Anything, mock.Anything).
		Return(acceptAppendEntries(1))

	n := newTestNode("node1", []string{"node2"}, transport)
	n.state = Leader
	n.term = 1
	n.logs = makeLogs(2, 1) // pre-existing entries
	n.matchIndex = map[string]int{n.config.ID: 2}
	n.nextIndex = map[string]int{"node2": 3}

	n.handleCommand([]byte("set y=2"))

	assert.Equal(t, 3, n.matchIndex[n.config.ID])
}

// TestHandleCommand_ReplicatesToAllPeers verifies that AppendEntries is sent
// to every peer, not just a subset.
func TestHandleCommand_ReplicatesToAllPeers(t *testing.T) {
	transport := &MockTransport{}
	transport.On("AppendEntries", mock.Anything, "node2", mock.Anything).
		Return(acceptAppendEntries(1))
	transport.On("AppendEntries", mock.Anything, "node3", mock.Anything).
		Return(acceptAppendEntries(1))

	n := newTestNode("node1", []string{"node2", "node3"}, transport)
	n.state = Leader
	n.term = 1
	n.logs = []types.LogEntry{}
	n.matchIndex = map[string]int{n.config.ID: 0}
	n.nextIndex = map[string]int{"node2": 1, "node3": 1}

	n.handleCommand([]byte("del z"))

	// Give goroutines time to fire
	time.Sleep(50 * time.Millisecond)

	transport.AssertCalled(t, "AppendEntries", mock.Anything, "node2", mock.Anything)
	transport.AssertCalled(t, "AppendEntries", mock.Anything, "node3", mock.Anything)
}

// TestSendAppendEntries_PostsResponseEvent verifies that a successful
// AppendEntries RPC results in an appendEntriesResponseEvent on n.events.
func TestSendAppendEntries_PostsResponseEvent(t *testing.T) {
	transport := &MockTransport{}
	transport.On("AppendEntries", mock.Anything, "node2", mock.Anything).
		Return(acceptAppendEntries(1))

	n := newTestNode("node1", []string{"node2"}, transport)
	n.term = 1

	req := &types.AppendEntriesRequest{}
	go n.sendAppendEntries("node2", "node2", req)

	select {
	case e := <-n.events:
		evt, ok := e.(appendEntriesResponseEvent)
		assert.True(t, ok, "expected appendEntriesResponseEvent")
		assert.Equal(t, "node2", evt.peer)
		assert.True(t, evt.resp.Success)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for appendEntriesResponseEvent")
	}
}

// TestSendAppendEntries_DropsOnError verifies that a transport error causes
// sendAppendEntries to return without posting anything to n.events.
func TestSendAppendEntries_DropsOnError(t *testing.T) {
	transport := &MockTransport{}
	transport.On("AppendEntries", mock.Anything, "node2", mock.Anything).
		Return(nil, assert.AnError)

	n := newTestNode("node1", []string{"node2"}, transport)

	req := &types.AppendEntriesRequest{}
	go n.sendAppendEntries("node2", "node2", req)

	select {
	case e := <-n.events:
		t.Fatalf("expected no event on error, got %T", e)
	case <-time.After(200 * time.Millisecond):
		// correct — nothing was posted
	}
}

// TestHandleCommand_NoPeers verifies that a single-node cluster still appends
// and updates matchIndex without panicking.
func TestHandleCommand_NoPeers(t *testing.T) {
	transport := &MockTransport{}

	n := newTestNode("node1", []string{}, transport)
	n.state = Leader
	n.term = 2
	n.logs = []types.LogEntry{}
	n.matchIndex = map[string]int{n.config.ID: 0}
	n.nextIndex = map[string]int{}

	assert.NotPanics(t, func() {
		n.handleCommand([]byte("ping"))
	})

	assert.Len(t, n.logs, 1)
	assert.Equal(t, 1, n.matchIndex[n.config.ID])
	transport.AssertNotCalled(t, "AppendEntries", mock.Anything, mock.Anything, mock.Anything)
}

// // ── Follower Recieves Append Entries ───────────────────────────────────────────

func TestHandleAE_HigherTermUpdatesTermAndClearsVote(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 1
	n.state = Follower
	n.votedFor = "node2"

	req := &types.AppendEntriesRequest{
		Term:         3,
		LeaderId:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}

	n.handleAppendEntries(req)

	assert.Equal(t, 3, n.term)
	assert.Equal(t, "", n.votedFor)
	assert.Equal(t, n.state, Follower)
	assert.Equal(t, n.leaderId, req.LeaderId)
}

func TestHandleAE_NewerTermBecomesFollower(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 2
	n.state = Candidate

	req := &types.AppendEntriesRequest{
		Term:         2,
		LeaderId:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}
	n.handleAppendEntries(req)

	assert.Equal(t, Follower, n.state)
	assert.Equal(t, "node2", n.leaderId)
}

// TestHandleAE_SameTermBecomesFollower verifies that matching term causes the
// node to transition to Follower and record the leader ID.
func TestHandleAE_SameTermBecomesFollower(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 2
	n.state = Candidate

	req := &types.AppendEntriesRequest{
		Term:         2,
		LeaderId:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}
	n.handleAppendEntries(req)

	assert.Equal(t, Follower, n.state)
	assert.Equal(t, "node2", n.leaderId)
}

// // ── logOk — rejection cases ───────────────────────────────────────────────────

// TestHandleAE_RejectWhenLogTooShort verifies that a request referencing a
// PrevLogIndex beyond the follower's log length is rejected.
func TestHandleAE_RejectWhenLogTooShort(t *testing.T) {
	follower_node := newTestNode("node1", []string{"node2"}, nil)
	follower_node.term = 1
	follower_node.logs = makeLogs(2, 1)

	leader_req := &types.AppendEntriesRequest{
		Term:         1,
		LeaderId:     "node2",
		PrevLogIndex: 5, // follower doesn't have entry 5
		PrevLogTerm:  1,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}
	resp := follower_node.handleAppendEntries(leader_req)

	assert.False(t, resp.Success)
	assert.Equal(t, 0, resp.Ack)
}

// TestHandleAE_RejectWhenPrevLogTermMismatch verifies that a request is
// rejected when the term at PrevLogIndex doesn't match PrevLogTerm.
func TestHandleAE_RejectWhenPrevLogTermMismatch(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 2
	n.logs = makeLogs(3, 1) // all entries have term=1

	req := &types.AppendEntriesRequest{
		Term:         2,
		LeaderId:     "node2",
		PrevLogIndex: 3,
		PrevLogTerm:  2, // follower has term=1 at index 3
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}
	resp := n.handleAppendEntries(req)

	assert.False(t, resp.Success)
	assert.Equal(t, 0, resp.Ack)
}

// TestHandleAE_RejectWhenTermStale verifies that a request with a lower term
// than the follower's current term is rejected outright.
func TestHandleAE_RejectWhenTermStale(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 5

	req := &types.AppendEntriesRequest{
		Term:         2, // stale
		LeaderId:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}
	resp := n.handleAppendEntries(req)

	assert.False(t, resp.Success)
	assert.Equal(t, 0, resp.Ack)
}

// // ── logOk — acceptance cases ──────────────────────────────────────────────────

// TestHandleAE_AcceptWhenPrevLogIndexZero verifies that a request with
// PrevLogIndex=0 (first ever entries) always passes the log check.
func TestHandleAE_AcceptWhenPrevLogIndexZero(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 1
	n.logs = []types.LogEntry{}

	req := &types.AppendEntriesRequest{
		Term:         1,
		LeaderId:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		LeaderCommit: 0,
		Entries:      makeLogs(2, 1),
	}
	resp := n.handleAppendEntries(req)

	assert.True(t, resp.Success)
	assert.Equal(t, 2, resp.Ack) // 0 + 2 entries
}

// TestHandleAE_AcceptWithMatchingPrevLogTerm verifies acceptance when
// PrevLogIndex and PrevLogTerm both match the follower's log.
func TestHandleAE_AcceptWithMatchingPrevLogTerm(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 2
	n.logs = makeLogs(3, 1) // 3 entries, all term=1

	req := &types.AppendEntriesRequest{
		Term:         2,
		LeaderId:     "node2",
		PrevLogIndex: 3,
		PrevLogTerm:  1, // matches n.logs[2].Term
		LeaderCommit: 0,
		Entries:      makeLogs(2, 2),
	}
	resp := n.handleAppendEntries(req)

	assert.True(t, resp.Success)
	assert.Equal(t, 5, resp.Ack) // PrevLogIndex(3) + len(Entries)(2)
}

// ── Ack calculation ───────────────────────────────────────────────────────────

// TestHandleAE_AckIsHeartbeat verifies that a heartbeat (no entries) returns
// Ack = PrevLogIndex with no log mutation.
func TestHandleAE_AckIsHeartbeat(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 1
	n.logs = makeLogs(4, 1)

	req := &types.AppendEntriesRequest{
		Term:         1,
		LeaderId:     "node2",
		PrevLogIndex: 4,
		PrevLogTerm:  1,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{}, // heartbeat
	}
	resp := n.handleAppendEntries(req)

	assert.True(t, resp.Success)
	assert.Equal(t, 4, resp.Ack) // PrevLogIndex + 0
	assert.Len(t, n.logs, 4)     // no new entries appended
}

// // ── Response fields ───────────────────────────────────────────────────────────

// TestHandleAE_ResponseContainsFollowerIdAndTerm verifies that both success
// and failure responses always carry the node's own ID and current term.
func TestHandleAE_ResponseContainsFollowerIdAndTerm(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)
	n.term = 3

	// Success case
	req := &types.AppendEntriesRequest{
		Term:         3,
		LeaderId:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		LeaderCommit: 0,
		Entries:      []types.LogEntry{},
	}
	resp := n.handleAppendEntries(req)
	assert.Equal(t, "node1", resp.FollowerId)
	assert.Equal(t, 3, resp.Term)

	// Failure case
	n.term = 3
	req.Term = 1 // stale
	resp = n.handleAppendEntries(req)
	assert.Equal(t, "node1", resp.FollowerId)
	assert.Equal(t, 3, resp.Term)
}

// ── replicate log ───────────────────────────────────────────────────────────

func TestHandleAE_ReplicateLogWithEmptyInitalLog(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)

	req := n.replicateLog("node2")

	assert.Equal(t, 0, req.PrevLogIndex)
	assert.Equal(t, 0, req.PrevLogTerm)
	assert.Empty(t, req.Entries)
}

func TestHandleAE_ReplicateLogWithOutdatedFollowerLog(t *testing.T) {
	n := newTestNode("node1", []string{"node2"}, nil)

	n.logs = makeSequentialLogs(7, 1)
	n.nextIndex = make(map[string]int)

	// follower only has 3 entries
	n.nextIndex["node2"] = 3

	req := n.replicateLog("node2")

	assert.Equal(t, 3, req.PrevLogIndex)
	assert.Equal(t, 3, req.PrevLogTerm)
	assert.Equal(t, n.logs[3:], req.Entries)
}
