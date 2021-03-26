package stratum

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"regexp"
	"strings"
	"sync/atomic"

	"xcoin/HayekPool/util"
)

var noncePattern *regexp.Regexp
var noncePattern64Bit *regexp.Regexp

const defaultWorkerId = "0"

func init() {
	noncePattern, _ = regexp.Compile("^[0-9a-f]{8}$")
	noncePattern64Bit, _ = regexp.Compile("^[0-9a-f]{16}$")
}

func (s *StratumServer) handleLoginRPC(cs *Session, params *LoginParams) (*JobReply, *ErrorReply) {
	address, id := extractWorkerId(params.Login)
	if !s.config.BypassAddressValidation && !util.ValidateAddress(address, s.config.Address) {
		log.Printf("Invalid address %s used for login by %s", address, cs.ip)
		return nil, &ErrorReply{Code: -1, Message: "Invalid address used for login"}
	}

	t := s.currentBlockTemplate()
	if t == nil {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}

	miner, ok := s.miners.Get(id)
	if !ok {
		miner = NewMiner(id, cs.ip)
		s.registerMiner(miner)
	}

	log.Printf("Miner connected %s@%s", params.Login, cs.ip)

	s.registerSession(cs)
	cs.login = params.Login
	miner.heartbeat()

	rjob := cs.getJob(t)
	if len(rjob.Blob) == 0 {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}
	return &JobReply{Id: id, Job: rjob, Status: "OK"}, nil
}

func (s *StratumServer) handleGetJobRPC(cs *Session, params *GetJobParams) (*JobReplyData, *ErrorReply) {
	miner, ok := s.miners.Get(params.Id)
	if !ok {
		return nil, &ErrorReply{Code: -1, Message: "Unauthenticated"}
	}
	t := s.currentBlockTemplate()
	if t == nil {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}
	miner.heartbeat()
	rjob := cs.getJob(t)
	if len(rjob.Blob) == 0 {
		return nil, &ErrorReply{Code: -1, Message: "Job not ready"}
	}

	return rjob, nil
}

func (s *StratumServer) handleSubmitRPC(cs *Session, params *SubmitParams) (statusReply *StatusReply, errReply *ErrorReply) {
	miner, ok := s.miners.Get(params.Id)
	if !ok {
		return nil, &ErrorReply{Code: -1, Message: "Unauthenticated"}
	}
	miner.heartbeat()

	job := cs.findJob(params.JobId)
	if job == nil {
		return nil, &ErrorReply{Code: -1, Message: "Invalid job id"}
	}

	switch {
	case noncePattern.MatchString(params.Nonce):
		// 填充高 32bit 的 job.extraNonce
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, job.extraNonce)
		params.Nonce = params.Nonce + hex.EncodeToString(b)

	case noncePattern64Bit.MatchString(params.Nonce):
		// 如果 nonce 已经是 64bit 则不用填充

	default:
		return nil, &ErrorReply{Code: -1, Message: "Malformed nonce"}
	}

	nonce := strings.ToLower(params.Nonce)
	exist := job.submit(nonce)
	if exist {
		atomic.AddInt64(&miner.invalidShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Duplicate share"}
	}

	t := s.currentBlockTemplate()
	if job.height != t.Height {
		log.Printf("Stale share for height %d from %s@%s", job.height, cs.login, cs.ip)
		atomic.AddInt64(&miner.staleShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Block expired"}
	}

	mineMode := 0
	if len(params.Seed) > 0 {
		mineMode = 1
	}

	storageExist, validShare := miner.processShare(s, cs, job, t, nonce, params.Result, mineMode)
	if !validShare {
		return nil, &ErrorReply{Code: -1, Message: "Low difficulty share"}
	}
	if storageExist {
		atomic.AddInt64(&miner.invalidShares, 1)
		return nil, &ErrorReply{Code: -1, Message: "Duplicate Share !"}
	}
	return &StatusReply{Status: "OK"}, nil
}

func (s *StratumServer) handleUnknownRPC(req *JSONRpcReq) *ErrorReply {
	log.Printf("Unknown RPC method: %v", req)
	return &ErrorReply{Code: -1, Message: "Invalid method"}
}

func (s *StratumServer) broadcastNewJobs() {
	t := s.currentBlockTemplate()
	if t == nil {
		return
	}
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	count := len(s.sessions)
	log.Printf("Broadcasting new jobs to %d miners", count)
	bcast := make(chan int, 1024*16)
	n := 0

	for m := range s.sessions {
		n++
		bcast <- n
		go func(cs *Session) {
			reply := cs.getNewJob(t)
			err := cs.pushMessage("mining.notify", &reply)
			<-bcast
			if err != nil {
				log.Printf("Job transmit error to %s: %v", cs.ip, err)
				s.removeSession(cs)
			} else {
				s.setDeadline(cs.conn)
			}
		}(m)
	}
}

func (s *StratumServer) refreshBlockTemplate(bcast bool) {
	newBlock := s.fetchBlockTemplate()
	if newBlock && bcast {
		s.broadcastNewJobs()
	}
}

func extractWorkerId(loginWorkerPair string) (string, string) {
	parts := strings.SplitN(loginWorkerPair, ".", 2)
	if len(parts) > 1 {
		return parts[0], parts[1]
	}
	return loginWorkerPair, defaultWorkerId
}
