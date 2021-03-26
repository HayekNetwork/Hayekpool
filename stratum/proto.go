package stratum

import (
	"encoding/json"
)

type JSONRpcReq struct {
	Id     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params *json.RawMessage `json:"params"`
}

type JSONRpcResp struct {
	Id      *json.RawMessage `json:"id"`
	Version string           `json:"jsonrpc"`
	Result  interface{}      `json:"result"`
	Error   interface{}      `json:"error"`
}

type JSONPushMessage struct {
	Version string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type LoginParams struct {
	Login string `json:"login"`
	Pass  string `json:"pass"`
	Agent string `json:"agent"`
}

type GetJobParams struct {
	Id string `json:"id"`
}

type SubmitParams struct {
	Id     string `json:"id"`
	JobId  string `json:"job_id"`
	Nonce  string `json:"nonce"`
	Result string `json:"result"`
	Seed   string `json:"seed"`
}

type ConfigReply struct {
		VersionRolling bool `json:"version-rolling"`
		VersionRollingMask string `json:"version-rolling.mask"`
		VersionRollingBit string `json:"version-rolling.min-bit-count"`
}
type JobReply struct {
	Id     string        `json:"id"`
	Job    *JobReplyData `json:"job"`
	Status string        `json:"status"`
}

type JobReplyData struct {
	Blob   string `json:"blob"`
	JobId  string `json:"job_id"`
	Target string `json:"target"`
	Height uint64 `json:"height"`
}

type StatusReply struct {
	Status string `json:"status"`
}

type ErrorReply struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type MiningJob struct {
	JobId string
	PrevHashReversed string
	Coinbase1 string
	CoinBase2 string
	MerkleBranch []string
	Version string
	NBits string
	Timestamp string
	CleanJob bool
}