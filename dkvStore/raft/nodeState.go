package raft

type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

var stateName = map[NodeState]string{
	Follower:  "follower",
	Candidate: "candidate",
	Leader:    "leader",
}

func (state NodeState) String() string {
	return stateName[state]
}
