package raft

import (
	"context"
	"math/rand"
	"sync"
	"time"
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
