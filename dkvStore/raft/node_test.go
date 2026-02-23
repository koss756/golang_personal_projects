package raft

import (
	"context"
	"testing"

	"github.com/koss756/dkvStore/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ---- Mock Transport ----

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
func newTestNode(id int, peers []string, transport Client) *Node {
	return &Node{
		id:                  id,
		state:               Follower,
		term:                0,
		peers:               peers,
		transport:           transport,
		events:              make(chan event, 10),
		resetElectionTimer:  make(chan struct{}, 1),
		resetHeartbeatTimer: make(chan struct{}, 1),
		config: Config{
			ElectionTimeoutLowerBound: 150,
			ElectionTimeoutUpperBound: 300,
			HeartbeatTimeout:          50,
		},
	}
}

// ---- Election Timeout Tests ----

// ---- Election Timeout Tests ----

func TestElectionTimeout_WinsElection_BecomesLeader(t *testing.T) {
	transport := new(MockTransport)
	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(&types.RequestVoteResponse{Term: 1, VoteGranted: true}, nil)

	node := newTestNode(1, []string{"peer2", "peer3"}, transport)
	node.handleElectionTimeout()

	assert.Equal(t, Leader, node.state)
	assert.Equal(t, 1, node.term)
	transport.AssertExpectations(t)
}

func TestElectionTimeout_TermIncrements(t *testing.T) {
	transport := new(MockTransport)
	transport.On("RequestVote", mock.Anything, mock.Anything, mock.Anything).
		Return(&types.RequestVoteResponse{Term: 3, VoteGranted: false}, nil)

	node := newTestNode(1, []string{"peer2", "peer3", "peer4", "peer5"}, transport)
	node.term = 2
	node.handleElectionTimeout()

	assert.Equal(t, 3, node.term)
	transport.AssertExpectations(t)
}
