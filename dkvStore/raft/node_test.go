package raft

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/koss756/dkvStore/types"
)

// mockClient is a mock implementation of the Client interface for testing
type mockClient struct {
	mu              sync.Mutex
	requestVoteFunc func(ctx context.Context, target string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error)
	voteResponses   map[string]*types.RequestVoteResponse
	voteErrors      map[string]error
}

func newMockClient() *mockClient {
	return &mockClient{
		voteResponses: make(map[string]*types.RequestVoteResponse),
		voteErrors:    make(map[string]error),
	}
}

func (m *mockClient) RequestVote(ctx context.Context, target string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.requestVoteFunc != nil {
		return m.requestVoteFunc(ctx, target, req)
	}

	if err, ok := m.voteErrors[target]; ok {
		return nil, err
	}

	if resp, ok := m.voteResponses[target]; ok {
		return resp, nil
	}

	// Default: grant vote
	return &types.RequestVoteResponse{
		Term:        req.Term,
		VoteGranted: true,
	}, nil
}

func (m *mockClient) AppendEntries(ctx context.Context, target string, req *types.AppendEntriesRequest) (*types.AppendEntriesResponse, error) {
	return &types.AppendEntriesResponse{
		Term:    req.Term,
		Success: true,
	}, nil
}

func (m *mockClient) setVoteResponse(target string, resp *types.RequestVoteResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.voteResponses[target] = resp
}

func (m *mockClient) setVoteError(target string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.voteErrors[target] = err
}

func (m *mockClient) setRequestVoteFunc(fn func(ctx context.Context, target string, req *types.RequestVoteRequest) (*types.RequestVoteResponse, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requestVoteFunc = fn
}

// Test helper to create a node with mock client
func createTestNode(id int, peers []string) (*Node, *mockClient) {
	mockClient := newMockClient()
	config := Config{
		ElectionTimeoutLowerBound: 150,
		ElectionTimeoutUpperBound:  300,
		HeartbeatTimeout:          50,
	}
	node := NewNode(id, peers, mockClient, config)
	return node, mockClient
}

// Test helper to process events synchronously
func processEvent(node *Node, ev event) {
	node.events <- ev
	// Give it a moment to process
	time.Sleep(10 * time.Millisecond)
}

func TestHandleRequestVote_StaleTerm(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.term = 5

	req := &types.RequestVoteRequest{
		Term:        3,
		CandidateID: 2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)

	if resp.VoteGranted {
		t.Errorf("Expected vote to be denied for stale term, got granted")
	}
	if resp.Term != 5 {
		t.Errorf("Expected term 5, got %d", resp.Term)
	}
	if node.votedFor != 0 {
		t.Errorf("Expected votedFor to remain 0, got %d", node.votedFor)
	}
}

func TestHandleRequestVote_NewerTerm(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.term = 3
	node.state = Candidate
	node.votedFor = 1

	req := &types.RequestVoteRequest{
		Term:        5,
		CandidateID: 2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)

	if node.term != 5 {
		t.Errorf("Expected term to be updated to 5, got %d", node.term)
	}
	if node.state != Follower {
		t.Errorf("Expected state to be Follower, got %s", node.state)
	}
	// When a newer term comes in, votedFor is reset to 0, then vote is granted and set to candidate ID
	if node.votedFor != 2 {
		t.Errorf("Expected votedFor to be set to candidate ID (2), got %d", node.votedFor)
	}
	if !resp.VoteGranted {
		t.Errorf("Expected vote to be granted for newer term, got denied")
	}
	if resp.Term != 5 {
		t.Errorf("Expected response term 5, got %d", resp.Term)
	}
}

func TestHandleRequestVote_NoPreviousVote(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.term = 3
	node.votedFor = 0

	req := &types.RequestVoteRequest{
		Term:        3,
		CandidateID: 2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)

	if !resp.VoteGranted {
		t.Errorf("Expected vote to be granted, got denied")
	}
	if node.votedFor != 2 {
		t.Errorf("Expected votedFor to be 2, got %d", node.votedFor)
	}
}

func TestHandleRequestVote_AlreadyVotedForSameCandidate(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.term = 3
	node.votedFor = 2

	req := &types.RequestVoteRequest{
		Term:        3,
		CandidateID: 2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)

	if !resp.VoteGranted {
		t.Errorf("Expected vote to be granted for same candidate, got denied")
	}
	if node.votedFor != 2 {
		t.Errorf("Expected votedFor to remain 2, got %d", node.votedFor)
	}
}

func TestHandleRequestVote_AlreadyVotedForDifferentCandidate(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.term = 3
	node.votedFor = 3

	req := &types.RequestVoteRequest{
		Term:        3,
		CandidateID: 2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	resp := node.handleRequestVote(req)

	if resp.VoteGranted {
		t.Errorf("Expected vote to be denied (already voted for different candidate), got granted")
	}
	if node.votedFor != 3 {
		t.Errorf("Expected votedFor to remain 3, got %d", node.votedFor)
	}
}

func TestHandleElectionTimeout_FollowerBecomesCandidate(t *testing.T) {
	node, mockClient := createTestNode(1, []string{"peer1"})
	node.state = Follower
	node.term = 1
	node.votedFor = 0

	// With 1 peer, votesNeeded = 1, so need 2 total votes (1 self + 1 peer)
	// Deny vote so it doesn't become leader
	mockClient.setVoteResponse("peer1", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: false,
	})

	node.handleElectionTimeout()

	// Should become candidate, then fail election and revert to follower
	if node.state != Follower {
		t.Errorf("Expected state to be Follower after failed election, got %s", node.state)
	}
	if node.term != 2 {
		t.Errorf("Expected term to be incremented to 2, got %d", node.term)
	}
	if node.votedFor != 0 {
		t.Errorf("Expected votedFor to be reset to 0 after failed election, got %d", node.votedFor)
	}
}

func TestHandleElectionTimeout_CandidateWithEnoughVotesBecomesLeader(t *testing.T) {
	node, mockClient := createTestNode(1, []string{"peer1", "peer2"})
	node.state = Follower
	node.term = 1

	// Set up mock to grant votes from both peers
	mockClient.setVoteResponse("peer1", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})
	mockClient.setVoteResponse("peer2", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})

	node.handleElectionTimeout()

	// With 2 peers, votesNeeded = (2/2) + 1 = 2
	// Node votes for itself (1 vote) + 2 peer votes = 3 votes > 2, should become leader
	if node.state != Leader {
		t.Errorf("Expected state to be Leader, got %s", node.state)
	}
	if node.term != 2 {
		t.Errorf("Expected term to be 2, got %d", node.term)
	}
}

func TestHandleElectionTimeout_CandidateWithInsufficientVotesRemainsFollower(t *testing.T) {
	node, mockClient := createTestNode(1, []string{"peer1", "peer2", "peer3"})
	node.state = Follower
	node.term = 1

	// Set up mock to deny votes from all peers
	mockClient.setVoteResponse("peer1", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: false,
	})
	mockClient.setVoteResponse("peer2", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: false,
	})
	mockClient.setVoteResponse("peer3", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: false,
	})

	node.handleElectionTimeout()

	// With 3 peers, votesNeeded = (3/2) + 1 = 2
	// Node votes for itself (1 vote) + 0 peer votes = 1 vote < 2, should remain follower
	if node.state != Follower {
		t.Errorf("Expected state to be Follower, got %s", node.state)
	}
	if node.votedFor != 0 {
		t.Errorf("Expected votedFor to be reset to 0, got %d", node.votedFor)
	}
}

func TestHandleElectionTimeout_LeaderIgnoresElectionTimeout(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.state = Leader
	node.term = 5
	initialTerm := node.term

	node.handleElectionTimeout()

	if node.state != Leader {
		t.Errorf("Expected state to remain Leader, got %s", node.state)
	}
	if node.term != initialTerm {
		t.Errorf("Expected term to remain %d, got %d", initialTerm, node.term)
	}
}

func TestHandleElectionTimeout_PartialVotes(t *testing.T) {
	node, mockClient := createTestNode(1, []string{"peer1", "peer2", "peer3", "peer4"})
	node.state = Follower
	node.term = 1

	// Grant votes from 3 out of 4 peers (need more than votesNeeded which is 3)
	mockClient.setVoteResponse("peer1", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})
	mockClient.setVoteResponse("peer2", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})
	mockClient.setVoteResponse("peer3", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})
	mockClient.setVoteResponse("peer4", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: false,
	})

	node.handleElectionTimeout()

	// With 4 peers, votesNeeded = (4/2) + 1 = 3
	// Node votes for itself (1 vote) + 3 peer votes = 4 votes > 3, should become leader
	if node.state != Leader {
		t.Errorf("Expected state to be Leader with enough votes, got %s", node.state)
	}
}

func TestHandleElectionTimeout_NetworkErrors(t *testing.T) {
	node, mockClient := createTestNode(1, []string{"peer1", "peer2", "peer3"})
	node.state = Follower
	node.term = 1

	// Simulate network errors from some peers
	mockClient.setVoteError("peer1", context.DeadlineExceeded)
	mockClient.setVoteResponse("peer2", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})
	mockClient.setVoteResponse("peer3", &types.RequestVoteResponse{
		Term:        2,
		VoteGranted: true,
	})

	node.handleElectionTimeout()

	// With 3 peers, votesNeeded = (3/2) + 1 = 2
	// Node votes for itself (1 vote) + 2 successful peer votes = 3 votes > 2, should become leader
	if node.state != Leader {
		t.Errorf("Expected state to be Leader despite network errors, got %s", node.state)
	}
}

func TestRequestVoteEvent_Integration(t *testing.T) {
	node, _ := createTestNode(1, []string{"peer1", "peer2"})
	node.term = 3

	// Start the event loop
	go node.Start()

	req := &types.RequestVoteRequest{
		Term:        3,
		CandidateID: 2,
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	respChan := make(chan *types.RequestVoteResponse, 1)
	ev := requestVoteEvent{
		req:  req,
		resp: respChan,
	}

	node.events <- ev

	select {
	case resp := <-respChan:
		if !resp.VoteGranted {
			t.Errorf("Expected vote to be granted, got denied")
		}
		if resp.Term != 3 {
			t.Errorf("Expected term 3, got %d", resp.Term)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for vote response")
	}
}

