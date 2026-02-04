package api

// func StartServer() {
// 	lis, err := net.Listen("tcp", ":9000")

// 	if err != nil {
// 		log.Fatalf("Server Failes to listen on port 9000: %v", err)
// 	}

// 	s := vote.Server{}

// 	grpcServer := grpc.NewServer()

// 	vote.RegisterVoteServiceServer(grpcServer, &s)

// 	if err = grpcServer.Serve(lis); err != nil {
// 		log.Fatalf("Failed to serve gRPC over port 900: %v", err)
// 	}
// }

// func ConnectToPeer() {
// 	var conn *grpc.ClientConn
// 	conn, err := grpc.Dial(":9000", grpc.WithInsecure())

// 	if err != nil {
// 		log.Fatalf("Could not connect: %s", err)
// 	}
// 	defer conn.Close()

// 	c := vote.NewVoteServiceClient(conn)

// 	msg := vote.RequestVoteMsg{CandidateId: 1, Term: 1, LastLogIndex: 1, LastLogTerm: 1}

// 	response, err := c.RequestVote(context.Background(), &msg)

// 	if err != nil {
// 		log.Fatalf("Error when requesting Vote: %s", err)
// 	}

// 	log.Printf("Response from Server: %s", response)
// }
