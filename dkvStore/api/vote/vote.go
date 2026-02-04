package vote

import (
	"log"

	"context"
)

type Server struct {
	UnimplementedVoteServiceServer
}

func (s *Server) RequestVote(ctx context.Context, msg *RequestVoteMsg) (*RequestVoteMsg, error) {
	log.Printf("Received Message body from client: %s", msg)

	return &RequestVoteMsg{CandidateId: 1, Term: 1, LastLogIndex: 1, LastLogTerm: 1}, nil
}
