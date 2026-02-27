package raft

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/koss756/dkvStore/types"
)

func randomizedTimeout(lowerBound, upperBound int) int {
	return rand.Intn(upperBound-lowerBound+1) + lowerBound
}

func broadcastToPeers(
	timeout time.Duration,
	peers []string,
	fn func(ctx context.Context, peer string),
) {
	var wg sync.WaitGroup

	for _, peer := range peers {
		wg.Add(1)

		go func(p string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			fn(ctx, p)
		}(peer)
	}

	wg.Wait()
}

func acceptAppendEntry(ans bool, term int) *types.AppendEntriesResponse {
	return &types.AppendEntriesResponse{Term: term, Success: ans}
}

func acceptRequestVote(ans bool, term int) *types.RequestVoteResponse {
	return &types.RequestVoteResponse{Term: term, VoteGranted: ans}
}

func toValueSlice(entries []*types.LogEntry) []types.LogEntry {
	out := make([]types.LogEntry, len(entries))
	for i, e := range entries {
		out[i] = *e
	}
	return out
}
