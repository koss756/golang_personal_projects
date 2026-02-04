package raft

import "math/rand"

func randomizedTimeout(lowerBound, upperBound int) int {
	return rand.Intn(upperBound-lowerBound+1) + lowerBound
}
