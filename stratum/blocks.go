package stratum

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"xcoin/HayekPool/dsha256"

	"xcoin/HayekPool/common"
	"xcoin/HayekPool/util"
)

const maxBacklog = 3
const majorVersion = 0x02
const minorVersion = 0x00
const patchVersion = 0x01

type heightDiffPair struct {
	diff   *big.Int
	height uint64
}

type BlockTemplate struct {
	sync.RWMutex
	Header     string
	Seed       string
	Target     string
	Difficulty *big.Int
	Height     uint64
	nonces     map[string]bool
	headers    map[string]heightDiffPair
	Timestamp  uint32
	MerkRoot   string
}

type Block struct {
	difficulty  *big.Int
	hashNoNonce common.Hash
	nonce       uint64
	mixDigest   common.Hash
	number      uint64
}

func genHashSeed(hash []byte, nonce uint32, ts uint32, target uint32, merkroot []byte) []byte {
	/*
	 *     total seed length is 80 byte
	 *     seed in memory:
	 *     version | pre_hash | merkroot | timestamp  | target   |  nonce    |
	 *     --------|----------|----------|------------|----------|-----------|
	 *     4 bytes | 32 bytes | 32 bytes | 4 bytes    | 4 bytes  |  4 bytes  |
	 */
	seed := make([]byte, 80)
	seed[0] = 0x02
	seed[1] = 0x00
	seed[2] = 0x01
	seed[3] = 0x04

	copy(seed[4:4+32], hash)
	copy(seed[4+32:4+32+32], merkroot)
	binary.BigEndian.PutUint32(seed[4+32+32:4+32+32+4], ts)
	binary.BigEndian.PutUint32(seed[4+32+32+4:4+32+32+4+4], target)
	binary.BigEndian.PutUint32(seed[4+32+32+4+4:4+32+32+4+4+4], nonce)
	return seed
}

func (b *BlockTemplate) nextBlob(extraNonce uint32, instanceId []byte) string {
	target := dsha256.GetCompact(b.Difficulty)
	blobdata := genHashSeed(
		common.HexToHash(b.Header).Bytes(),
		extraNonce,
		b.Timestamp,
		target,
		common.HexToHash(b.MerkRoot).Bytes(),
		)
	return hex.EncodeToString(blobdata)
}

func (b Block) Difficulty() *big.Int     { return b.difficulty }
func (b Block) HashNoNonce() common.Hash { return b.hashNoNonce }
func (b Block) Nonce() uint64            { return b.nonce }
func (b Block) MixDigest() common.Hash   { return b.mixDigest }
func (b Block) NumberU64() uint64        { return b.number }

func (s *StratumServer) fetchBlockTemplate() bool {
	rpc := s.rpc()
	reply, err := rpc.GetWork()
	if err != nil {
		log.Printf("Error while refreshing block template on %s: %s", rpc.Name, err)
		return false
	}

	diff := dsha256.TargetToDiff(strings.Replace(reply[2], "0x", "", -1))
	height, err := strconv.ParseUint(strings.Replace(reply[3], "0x", "", -1), 16, 64)
	ts, err := strconv.ParseUint(strings.Replace(reply[5], "0x", "", -1), 16, 64)

	// No need to update, we have fresh job
	t := s.currentBlockTemplate()
	if t != nil {
		if height > t.Height {
			if t.Header == reply[0] {
				return false
			} else {
				log.Printf("New block to mine on %s at height %v, diff: %v", rpc.Name, height, diff)
			}
		} else {
			return false
		}
	}

	newTemplate := BlockTemplate{
		Header:     reply[0],
		Seed:       reply[1],
		Target:     reply[2],
		Height:     height,
		Difficulty: diff,
		headers:    make(map[string]heightDiffPair),
		Timestamp:  uint32(ts),
		MerkRoot:   reply[4],
	}
	// Copy job backlog and add current one
	newTemplate.headers[reply[0]] = heightDiffPair{
		diff:   util.TargetHexToDiff(reply[2]),
		height: height,
	}
	if t != nil {
		for k, v := range t.headers {
			if v.height > height-maxBacklog {
				newTemplate.headers[k] = v
			}
		}
	}

	s.blockTemplate.Store(&newTemplate)
	log.Printf("New block to mine on %s at height %d / hash %s / diff %d", rpc.Name, height, reply[0][0:10], diff)
	return true
}
