package types

type RequestVoteRequest struct {
	Term         int
	CandidateID  string
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteResponse struct {
	Term        int
	VoteGranted bool
}

type LogEntry struct {
	Term    int
	Command []byte
}

type AppendEntriesRequest struct {
	Term         int
	LeaderId     string
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesResponse struct {
	FollowerId string
	Term       int
	Ack        int
	Success    bool
}

type NodeStatus struct {
	State         string `json:"state"`
	Term          int    `json:"term"`
	VotedFor      string `json:"voted_for"`
	LeaderID      string `json:"leader_id"`
	LogLength     int    `json:"log_length"`
	VotesReceived int    `json:"votes_received"`
	VotesNeeded   int    `json:"votes_needed"`

	CommitIndex int `json:"commit_index"`
	LastApplied int `json:"last_applied"`

	Peers []string `json:"peers"`

	NextIndex  map[string]int `json:"next_index,omitempty"`
	MatchIndex map[string]int `json:"match_index,omitempty"`
}
