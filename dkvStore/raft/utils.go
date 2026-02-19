package raft

import (
	"bytes"
	"context"
	"encoding/gob"
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

// SerializeCommand converts a Command to []byte using gob encoding
func SerializeCommand(cmd Command) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(cmd)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DeserializeCommand converts []byte back to a Command using gob decoding
func DeserializeCommand(data []byte) (Command, error) {
	var cmd Command
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&cmd)
	return cmd, err
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
