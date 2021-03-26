package stratum

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"xcoin/HayekPool/common"
	"xcoin/HayekPool/dsha256"
	"xcoin/HayekPool/util"
)

type Job struct {
	height uint64
	sync.RWMutex
	id          string
	extraNonce  uint32
	submissions map[string]struct{}
}

type Miner struct {
	lastBeat      int64
	startedAt     int64
	validShares   int64
	invalidShares int64
	staleShares   int64
	accepts       int64
	rejects       int64
	shares        map[int64]int64
	sync.RWMutex
	id string
	ip string
}

func (job *Job) submit(nonce string) bool {
	job.Lock()
	defer job.Unlock()
	if _, exist := job.submissions[nonce]; exist {
		return true
	}
	job.submissions[nonce] = struct{}{}
	return false
}

func NewMiner(id string, ip string) *Miner {
	shares := make(map[int64]int64)
	return &Miner{id: id, ip: ip, shares: shares}
}

func (p *Session) addSubmitCount(delta int) {
	atomic.AddInt32(&p.submitCount, int32(delta))
}

// 获取难度
func (p *Session) getDifficulty() int64 {
	if !p.endpoint.config.AdaptiveDifficultyEnabled {
		return p.endpoint.config.Difficulty
	}

	return atomic.LoadInt64(&p.lastDifficulty)
}

// 设置难度
func (p *Session) setDifficulty(diff int64) {
	// 无效的验证区间
	if p.endpoint.config.StartDifficulty > p.endpoint.config.Difficulty {
		log.Fatal("invalid diff:", p.endpoint.config)
	}

	// 调整难度
	if diff < p.endpoint.config.StartDifficulty {
		diff = p.endpoint.config.StartDifficulty
	}
	if diff > p.endpoint.config.Difficulty {
		diff = p.endpoint.config.Difficulty
	}

	atomic.StoreInt64(&p.lastDifficulty, diff)
}

func (p *Session) updateDifficulty() {
	if !p.endpoint.config.AdaptiveDifficultyEnabled {
		return
	}

	var diff = p.getDifficulty()
	var lastJobUnixTime = atomic.LoadInt64(&p.lastJobUnixTime)
	var submitCount = atomic.LoadInt32(&p.submitCount)

	if lastJobUnixTime == 0 {
		atomic.StoreInt64(&p.lastJobUnixTime, time.Now().Unix())
		atomic.StoreInt32(&p.submitCount, 0)
		p.setDifficulty(p.endpoint.config.Difficulty)
		return
	}

	var seconds = time.Now().Unix() - lastJobUnixTime
	if seconds < 30 {
		return
	}

	var expectSubmitCount = int32(p.endpoint.config.SubmitRate * float64(seconds))
	switch {
	case submitCount > expectSubmitCount*2:
		atomic.StoreInt64(&p.lastJobUnixTime, time.Now().Unix())
		atomic.StoreInt32(&p.submitCount, 0)
		p.setDifficulty(int64(float64(diff) * 1.3))
		return

	case 2*submitCount < expectSubmitCount:
		atomic.StoreInt64(&p.lastJobUnixTime, time.Now().Unix())
		atomic.StoreInt32(&p.submitCount, 0)
		p.setDifficulty(int64(float64(diff) * 0.7))
		return

	default:
		atomic.StoreInt64(&p.lastJobUnixTime, time.Now().Unix())
		atomic.StoreInt32(&p.submitCount, 0)
	}
}

func (cs *Session) getJob(t *BlockTemplate) *JobReplyData {
	height := atomic.SwapUint64(&cs.lastBlockHeight, t.Height)
	_ = height

	if height == t.Height {
		return &JobReplyData{}
	}

	// 先更新难度
	cs.updateDifficulty()

	diff := cs.getDifficulty()

	extraNonce := atomic.AddUint32(&cs.endpoint.extraNonce, 1)
	blob := t.nextBlob(extraNonce, cs.endpoint.instanceId)
	id := atomic.AddUint64(&cs.endpoint.jobSequence, 1)
	job := &Job{
		id:         strconv.FormatUint(id, 10),
		extraNonce: extraNonce,
		height:     t.Height,
	}
	job.submissions = make(map[string]struct{})
	cs.pushJob(job)

	reply := &JobReplyData{
		JobId:  job.id,
		Blob:   blob,
		Target: cs.endpoint.targetHex,
		Height: t.Height,
	}
	if cs.endpoint.config.AdaptiveDifficultyEnabled {
		reply.Target = util.GetTargetHex(diff)
		//reply.Target = dsha256.SetCompact(diff) //TODO: use dynamic difficulty calculate
	}

	return reply
}

func (cs *Session) getNewJob(t *BlockTemplate) *MiningJob {
	height := atomic.SwapUint64(&cs.lastBlockHeight, t.Height)
	_ = height

	if height == t.Height {
		return &MiningJob{}
	}

	// 先更新难度
	cs.updateDifficulty()

	extraNonce := atomic.AddUint32(&cs.endpoint.extraNonce, 1)
	id := atomic.AddUint64(&cs.endpoint.jobSequence, 1)
	job := &Job{
		id:         strconv.FormatUint(id, 10),
		extraNonce: extraNonce,
		height:     t.Height,
	}
	job.submissions = make(map[string]struct{})
	cs.pushJob(job)
		reply := &MiningJob{
		JobId:            job.id,
		PrevHashReversed: t.Header,
		Coinbase1:        "",
		//Coinbase1:        "01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff64032fa4092cfabe6d6dc5cf100d10589275d705cb97a9bd1538a43aa3d80f79b2cef3df8bae83df9be910000000f09f909f082f4632506f6f6c2f104d696e656420627920676f61746269740000000000000000000000000000000000000005",
		CoinBase2:        "",
		//CoinBase2:        "046df52126000000001976a914c825a1ecf2a6830c4401620c3a16f1995057c2ab88ac00000000000000002f6a24aa21a9edd8b230477653c88496573b58cbaf3a3e9b94967780d2994711ca0351f84dbc3308000000000000000000000000000000002c6a4c2952534b424c4f434b3af8d08fcbbe5d9822db33f13c0eafd3a8346fd816d3a462790c1bbc23002479480000000000000000266a24b9e11b6dba3d0da4b316490cad28b9c143047d53fc9fffb5c6454aad22553bed229f604135512c3a",
		MerkleBranch:     nil,
		Version:          "00200020",
		NBits:            fmt.Sprintf("%x", dsha256.GetCompact(dsha256.DiffToTarget(t.Difficulty))),
		Timestamp:        fmt.Sprintf("%x", t.Timestamp), //TODO: check if need little Endian format
		CleanJob:         true,
	}
	if cs.endpoint.config.AdaptiveDifficultyEnabled {
		//reply.Target = util.GetTargetHex(diff)
		//reply.Target = dsha256.SetCompact(diff) //TODO: use dynamic difficulty calculate
	}
	return reply
}

func (cs *Session) pushJob(job *Job) {
	cs.Lock()
	defer cs.Unlock()
	cs.validJobs = append(cs.validJobs, job)

	if len(cs.validJobs) > 4 {
		cs.validJobs = cs.validJobs[1:]
	}
}

func (cs *Session) pushNewJob(job *MiningJob) {
	cs.Lock()
	defer cs.Unlock()
	cs.validNewJobs = append(cs.validNewJobs, job)

	if len(cs.validNewJobs) > 4 {
		cs.validNewJobs = cs.validNewJobs[1:]
	}
}

func (cs *Session) findJob(id string) *Job {
	cs.Lock()
	defer cs.Unlock()
	for _, job := range cs.validJobs {
		if job.id == id {
			return job
		}
	}
	return nil
}

func (cs *Session) findNewJob(id string) *MiningJob {
	cs.Lock()
	defer cs.Unlock()
	for _, job := range cs.validNewJobs {
		if job.JobId == id {
			return job
		}
	}
	return nil
}

func (m *Miner) heartbeat() {
	now := util.MakeTimestamp()
	atomic.StoreInt64(&m.lastBeat, now)
}

func (m *Miner) getLastBeat() int64 {
	return atomic.LoadInt64(&m.lastBeat)
}

func (m *Miner) storeShare(diff int64) {
	now := util.MakeTimestamp() / 1000
	m.Lock()
	m.shares[now] += diff
	m.Unlock()
}

func (m *Miner) hashrate(estimationWindow time.Duration) float64 {
	now := util.MakeTimestamp() / 1000
	totalShares := int64(0)
	window := int64(estimationWindow / time.Second)
	boundary := now - m.startedAt

	if boundary > window {
		boundary = window
	}

	m.Lock()
	for k, v := range m.shares {
		if k < now-86400 {
			delete(m.shares, k)
		} else if k >= now-window {
			totalShares += v
		}
	}
	m.Unlock()
	return float64(totalShares) / float64(boundary)
}

func (m *Miner) processShare(s *StratumServer, cs *Session, job *Job, t *BlockTemplate, mnonce string, result string, mineMode int) (bool, bool) {
	var nonceHex string
	if len(mnonce) > 8 {
		nonceHex = fmt.Sprintf("0x%s", mnonce)

	} else {

		nonceHex = fmt.Sprintf("0x%s00000000", mnonce)
	}

	hashNoNonce := t.Header
	mixDigest := result
	stateRoot := t.MerkRoot
	ts := t.Timestamp

	params := []string{nonceHex, t.Header, result}
	h3 := "0x" + params[2]
	shareDiff := cs.getDifficulty()

	nonce, _ := strconv.ParseUint(strings.Replace(nonceHex, "0x", "", -1), 16, 64)

	h, ok := t.headers[hashNoNonce]
	if !ok {
		log.Printf("Stale share from %v@%v", m.id, cs.ip)
		return false, false
	}

	share := Block{
		number:      h.height,
		hashNoNonce: common.HexToHash(hashNoNonce),
		difficulty:  big.NewInt(shareDiff),
		nonce:       nonce,
		mixDigest:   common.HexToHash(mixDigest),
	}

	merkroot := common.HexToHash(stateRoot)
	//digest, mix := hashcryptonight2020(share.hashNoNonce.Bytes(), share.nonce, ts, merkroot.Bytes())
	headerTarget := dsha256.DiffToTarget(share.difficulty)
	headerNBits := dsha256.GetCompact(headerTarget)
	digest, mix := dsha256.Dsha256(share.hashNoNonce.Bytes(), uint32(share.nonce), ts, headerNBits, merkroot.Bytes())
	params2Hash, _ := hex.DecodeString(result)

	if mineMode == 1 && !bytes.Equal([]byte(params2Hash), digest) {
		return false, false
	} else if mineMode == 0 && !bytes.Equal([]byte(params2Hash), mix) {
		return false, false
	}

	cs.Lock()
	var mixReserve []byte
	rLen := len(mix)
	for i := 0; i < rLen; i++ {
		mixReserve = append(mixReserve, mix[rLen-i-1])
	}
	cs.Unlock()

	if CheckHashDifficulty(mix, uint64(shareDiff)) {
		cs.addSubmitCount(1) // 统计成功提交的次数
		log.Printf("share accepted")
	} else {
		return false, false
	}

	if mineMode == 0 {
		h := fmt.Sprintf("0x%x", digest)
		params[2] = h
	} else {
		params[2] = h3
	}

	hexResult := hex.EncodeToString(mix)
	hexDigest := hex.EncodeToString(digest)

	bigResult,err := new(big.Int).SetString(hexResult, 16)
	if !err {
		log.Println("hex to big error")
	}
	orgTarget := dsha256.SetCompact(headerNBits)


	log.Println("Double Hash", "digest", hexDigest, "result", hexResult, "bigResult", bigResult, "headerTarget", orgTarget)

	if bigResult.Cmp(orgTarget) <= 0 {
		ok, err := s.rpc().SubmitBlock(params)
		if err != nil {
			log.Printf("Block submission failure at height %v for %v: %v", h.height, t.Header, err)
		} else if !ok {
			log.Printf("Block rejected at height %v for %v", h.height, t.Header)
			return false, false
		} else {
			exist, err := s.backend.WriteBlock(cs.login, m.id, params, shareDiff, h.diff.Int64(), t.Height, s.hashrateExpiration)
			if exist {
			}
			if err != nil {
				log.Println("Failed to insert block candidate into backend:", err)
			} else {
				log.Printf("Inserted block %v to backend", h.height)
			}
			s.refreshBlockTemplate(true)
			log.Printf("Block found by miner %v@%v at height %d", cs.login, cs.ip, h.height)
		}
	} else {
		exist, err := s.backend.WriteShare(cs.login, m.id, params, shareDiff, t.Height, s.hashrateExpiration)
		if exist {
		}
		if err != nil {
			log.Println("Failed to insert share data into backend:", err)
		}
	}

	return false, true
}
