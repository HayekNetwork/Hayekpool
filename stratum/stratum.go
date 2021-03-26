package stratum

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"xcoin/HayekPool/pool"
	"xcoin/HayekPool/rpc"
	"xcoin/HayekPool/storage"
	"xcoin/HayekPool/util"
)

type StratumServer struct {
	luckWindow       int64
	luckLargeWindow  int64
	roundShares      int64
	blockStats       map[int64]blockEntry
	config           *pool.Config
	miners           MinersMap
	blockTemplate    atomic.Value
	upstream         int32
	upstreams        []*rpc.RPCClient
	timeout          time.Duration
	estimationWindow time.Duration
	blocksMu         sync.RWMutex
	sessionsMu       sync.RWMutex
	sessions         map[*Session]struct{}
	//policy           *policy.PolicyServer
	failsCount         int64
	backend            *storage.RedisClient
	hashrateExpiration time.Duration
}

type blockEntry struct {
	height   int64
	variance float64
	hash     string
}

type Endpoint struct {
	jobSequence uint64
	config      *pool.Port
	difficulty  *big.Int
	instanceId  []byte
	extraNonce  uint32
	targetHex   string
}

type Session struct {
	lastBlockHeight uint64
	sync.Mutex
	conn      *net.TCPConn
	enc       *json.Encoder
	ip        string
	login     string
	endpoint  *Endpoint
	validJobs []*Job
	validNewJobs []*MiningJob

	lastDifficulty  int64 // 最近的难度
	lastJobUnixTime int64 // 最近收到 job 的时间
	submitCount     int32 // 最近 pushMessage(job) 之后的提交数目统计(用于计算动态难度)
	extraNonce1     []byte
}

const (
	MaxReqSize = 10 * 1024
)

func NewStratum(cfg *SConfig, backend *storage.RedisClient) *StratumServer {
	poolCfg := cfg.Pool
	stratum := &StratumServer{config: &poolCfg, blockStats: make(map[int64]blockEntry), backend: backend}

	stratum.upstreams = make([]*rpc.RPCClient, len(cfg.Pool.Upstream))
	for i, v := range cfg.Pool.Upstream {
		rawUrl := fmt.Sprintf("http://%s:%v", v.Host, v.Port)
		url, err := url.Parse(rawUrl)
		if err != nil {
			return nil
		}

		client, err := rpc.NewRPCClient(v.Name, url.String(), v.Timeout)
		if err != nil {
			log.Fatal(err)
		} else {
			stratum.upstreams[i] = client
			log.Printf("Upstream: %s => %s", client.Name, client.Url)
		}
	}
	log.Printf("Default upstream: %s => %s", stratum.rpc().Name, stratum.rpc().Url)

	stratum.hashrateExpiration = util.MustParseDuration(cfg.Pool.HashrateExpiration)

	stratum.miners = NewMinersMap()
	stratum.sessions = make(map[*Session]struct{})

	timeout, _ := time.ParseDuration(cfg.Pool.Stratum.Timeout)
	stratum.timeout = timeout

	estimationWindow, _ := time.ParseDuration(cfg.Pool.EstimationWindow)
	stratum.estimationWindow = estimationWindow

	luckWindow, _ := time.ParseDuration(cfg.Pool.LuckWindow)
	stratum.luckWindow = int64(luckWindow / time.Millisecond)
	luckLargeWindow, _ := time.ParseDuration(cfg.Pool.LargeLuckWindow)
	stratum.luckLargeWindow = int64(luckLargeWindow / time.Millisecond)

	refreshIntv, _ := time.ParseDuration(cfg.Pool.BlockRefreshInterval)
	refreshTimer := time.NewTimer(refreshIntv)
	log.Printf("Set block refresh every %v", refreshIntv)

	checkIntv, _ := time.ParseDuration(cfg.Pool.UpstreamCheckInterval)
	checkTimer := time.NewTimer(checkIntv)

	stateUpdateIntv := util.MustParseDuration(cfg.Pool.StateUpdateInterval)
	stateUpdateTimer := time.NewTimer(stateUpdateIntv)

	// Init block template
	go stratum.refreshBlockTemplate(false)

	go func() {
		for {
			select {
			case <-refreshTimer.C:
				stratum.refreshBlockTemplate(true)
				refreshTimer.Reset(refreshIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-checkTimer.C:
				stratum.checkUpstreams()
				checkTimer.Reset(checkIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-stateUpdateTimer.C:
				t := stratum.currentBlockTemplate()
				if t != nil {
					err := backend.WriteNodeState(cfg.Pool.Name, t.Height, t.Difficulty)
					if err != nil {
						log.Printf("Failed to write node state to backend: %v", err)
						stratum.markSick()
					} else {
						stratum.markOk()
					}
				}
				stateUpdateTimer.Reset(stateUpdateIntv)
			}
		}
	}()
	return stratum
}

func NewEndpoint(cfg *pool.Port) *Endpoint {
	e := &Endpoint{config: cfg}
	e.instanceId = make([]byte, 4)
	_, err := rand.Read(e.instanceId)
	if err != nil {
		log.Fatalf("Can't seed with random bytes: %v", err)
	}
	e.targetHex = util.GetTargetHex(e.config.Difficulty)
	e.difficulty = big.NewInt(e.config.Difficulty)
	return e
}

func (s *StratumServer) Listen() {
	quit := make(chan bool)
	for _, port := range s.config.Stratum.Ports {
		go func(cfg pool.Port) {
			e := NewEndpoint(&cfg)
			e.Listen(s)
		}(port)
	}
	<-quit
}

func (e *Endpoint) Listen(s *StratumServer) {
	bindAddr := fmt.Sprintf("%s:%d", e.config.Host, e.config.Port)
	addr, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	server, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	defer server.Close()

	log.Printf("Stratum listening on %s", bindAddr)
	accept := make(chan int, e.config.MaxConn)
	n := 0

	for {
		conn, err := server.AcceptTCP()
		if err != nil {
			continue
		}
		conn.SetKeepAlive(true)
		ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		cs := &Session{
			conn:     conn,
			ip:       ip,
			enc:      json.NewEncoder(conn),
			endpoint: e,
		}
		n += 1

		accept <- n
		go func() {
			s.handleClient(cs, e)
			<-accept
		}()
	}
}

func (s *StratumServer) handleClient(cs *Session, e *Endpoint) {
	connbuff := bufio.NewReaderSize(cs.conn, MaxReqSize)
	s.setDeadline(cs.conn)

	for {
		data, isPrefix, err := connbuff.ReadLine()
		if isPrefix {
			log.Println("Socket flood detected from", cs.ip)
			break
		} else if err == io.EOF {
			log.Println("Client disconnected", cs.ip)
			break
		} else if err != nil {
			log.Println("Error reading:", err)
			break
		}

		// NOTICE: cpuminer-multi sends junk newlines, so we demand at least 1 byte for decode
		// NOTICE: Ns*CNMiner.exe will send malformed JSON on very low diff, not sure we should handle this
		if len(data) > 1 {
			var req JSONRpcReq
			err = json.Unmarshal(data, &req)
			if err != nil {
				log.Printf("Malformed request from %s: %v", cs.ip, err)
				break
			}
			s.setDeadline(cs.conn)
			err = cs.handleMessage(s, e, &req)
			if err != nil {
				break
			}
		}
	}
	s.removeSession(cs)
	cs.conn.Close()
}

func (cs *Session) handleMessage(s *StratumServer, e *Endpoint, req *JSONRpcReq) error {
	if req.Id == nil {
		err := fmt.Errorf("server disconnect request")
		log.Println(err)
		return err
	} else if req.Params == nil {
		err := fmt.Errorf("server RPC request params")
		log.Println(err)
		return err
	}

	// Handle RPC methods
	switch req.Method {

	case "login":
		var params LoginParams
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Unable to parse params")
			return err
		}
		reply, errReply := s.handleLoginRPC(cs, &params)
		if errReply != nil {
			return cs.sendError(req.Id, errReply, true)
		}
		return cs.sendResult(req.Id, &reply)
	case "getjob":
		var params GetJobParams
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Unable to parse params")
			return err
		}
		reply, errReply := s.handleGetJobRPC(cs, &params)
		if errReply != nil {
			return cs.sendError(req.Id, errReply, true)
		}
		return cs.sendResult(req.Id, &reply)
	case "mining.submit":
		var params SubmitParams
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Unable to parse params")
			return err
		}
		reply, errReply := s.handleSubmitRPC(cs, &params)
		if errReply != nil {
			return cs.sendError(req.Id, errReply, false)
		}
		return cs.sendResult(req.Id, &reply)
	case "keepalived":
		return cs.sendResult(req.Id, &StatusReply{Status: "KEEPALIVED"})
	case "mining.configure":
		var params []interface{}
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Unable to parse Configure params", err)
			return err
		}
		log.Println(params)
		log.Println("reqId:", string(*req.Id), "req.Method:",string(req.Method), "req.params:", params[0], params[1])

		for _, v := range params{
			if m, ok := v.(map[string]interface{}); ok {
				log.Println(m["version-rolling.mask"], m["version-rolling.min-bit-count"])
			}
		}
		return cs.sendResult(req.Id, &ConfigReply{
			VersionRolling:     true,
			VersionRollingMask: "1fffe000",
			VersionRollingBit: "16",
		})
		/*
		return cs.sendError(req.Id, &ErrorReply{
			Code:    -1,
			Message: "configure error",
		}, true)

		 */
		case "mining.subscribe":
			var params []interface{}
			err := json.Unmarshal(*req.Params, &params)
			if err != nil {
				log.Println("Unable to parse Configure params", err)
				return err
			}
			type subscribeParams struct {
				Id int `json:"id"`
				Result []interface{} `json:"result"`
				Error []interface{} `json:"error"`
			}
			log.Println("mining.subscribe req params", params)

			/*
			raw := `{"id":1,"result":[[["mining.notify","mining.notify"],["mining.set_difficulty","mining.set_difficulty"]],"00",8],"error":null}`
			result := &subscribeParams{}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				log.Println("json unmarshal failed")
				return cs.sendError(req.Id, &ErrorReply{
					Code:    -1,
					Message: "subscribe failed unmashal json data",
				}, true)
			}
			log.Println("mining.subscribe response params",result)
			return cs.sendResult(req.Id, result.Result)

			 */
			extNonceGener := util.NewExtraNonce1Generator()
			subscriptionId := extNonceGener.GetExtraNonce1()
			//placeholder, _ := hex.DecodeString("f000000ff111111f")
		    //extraNonce2Size:= len(placeholder) - extNonceGener.Size
			return cs.sendResult(req.Id, []interface{}{
				[][]string{
					{"mining.set_difficulty", strconv.FormatUint(binary.LittleEndian.Uint64(subscriptionId), 10)},
					{"mining.notify", strconv.FormatUint(binary.LittleEndian.Uint64(subscriptionId), 10)},
				},
				//hex.EncodeToString(cs.extraNonce1),
				//extraNonce2Size,
				"00",
				8,
			})
	case "mining.authorize":
		var params []interface{}
		err := json.Unmarshal(*req.Params, &params)
		if err != nil {
			log.Println("Unable to parse Configure params", err)
			return err
		}
		log.Println("mining.authorize params:", params[0])
		var loginParms = LoginParams{
		Login: params[0].(string),
		Pass:"",
		Agent:"",
		}

		_, errReply := s.handleLoginRPC(cs, &loginParms)
		if errReply != nil {
			log.Println("** Failed to handle login **")
			return cs.sendError(req.Id, errReply, true)
		}
		log.Println("login succeed!")
		_ = cs.sendResult(req.Id, true)

		log.Println("start push message:")
        diff := []int{1} //TODO: just for test, should replace with t.Diff
		cs.pushMessage("miming.set_difficulty", diff)

        //TODO:just for miner test
		rawMessage := `{"id":null,"method":"mining.notify","params":["B8rBfBgte","0000000000000000000000000000000000000000000000000000000000000000","01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff64032fa4092cfabe6d6dc5cf100d10589275d705cb97a9bd1538a43aa3d80f79b2cef3df8bae83df9be910000000f09f909f082f4632506f6f6c2f104d696e656420627920676f61746269740000000000000000000000000000000000000005","046df52126000000001976a914c825a1ecf2a6830c4401620c3a16f1995057c2ab88ac00000000000000002f6a24aa21a9edd8b230477653c88496573b58cbaf3a3e9b94967780d2994711ca0351f84dbc3308000000000000000000000000000000002c6a4c2952534b424c4f434b3af8d08fcbbe5d9822db33f13c0eafd3a8346fd816d3a462790c1bbc23002479480000000000000000266a24b9e11b6dba3d0da4b316490cad28b9c143047d53fc9fffb5c6454aad22553bed229f604135512c3a",["cabe23c7b037fdc897f34263599c1955c81ea5eecac3e21b78f5d2b24433333b","b65a061b6b328db86fbba3bbe6fa229700a1bb3386325dff5e1da8abb011b670","1b18a0a020837e44ef5eb2ab72aa1a0fd02c89e5b92cde9efe727e72edee8065","a5fe7a2229dbfb460014b1253b62b9e278d60f54a60e33546272fd57377ea263","fc11bb0b3eb27a2e3a712a04d7dd53fde97080bb0bfbbc81ac180f427f195a4a","1fbe3c35951536c76f90437f6eb24a12a4c07741f1ed74e8b932d2b686288b56","2699f62eabc74331aeb6d2c1ec91a9e29991d9ecccd7eb6e29991eb47e0b31ce","defc0dcef5fd0c6b89445be995ffcc906723b3c88a8d1d05c0f83fc4ee14edea","cab447191f8cfefdadd893501181b1cffcf4f4770300e5f40a928da7c0fabe69","2bde21342f11358bd6d384b90928416f6100985a30e03c8bc649ca9f9567d67c"],"00200020","1d00ffff","605c136a",true]}`
		//rawMessage := `{"id":null,"method":"mining.notify","params":["B8rBfBgte","ca233f5e0d742ae5496c70bca70c29f2fc2e07f1000ccab20000000000000000","01000000010000000000000000000000000000000000000000000000000000000000000000ffffffff64032fa4092cfabe6d6dc5cf100d10589275d705cb97a9bd1538a43aa3d80f79b2cef3df8bae83df9be910000000f09f909f082f4632506f6f6c2f104d696e656420627920676f61746269740000000000000000000000000000000000000005","046df52126000000001976a914c825a1ecf2a6830c4401620c3a16f1995057c2ab88ac00000000000000002f6a24aa21a9edd8b230477653c88496573b58cbaf3a3e9b94967780d2994711ca0351f84dbc3308000000000000000000000000000000002c6a4c2952534b424c4f434b3af8d08fcbbe5d9822db33f13c0eafd3a8346fd816d3a462790c1bbc23002479480000000000000000266a24b9e11b6dba3d0da4b316490cad28b9c143047d53fc9fffb5c6454aad22553bed229f604135512c3a",["cabe23c7b037fdc897f34263599c1955c81ea5eecac3e21b78f5d2b24433333b","b65a061b6b328db86fbba3bbe6fa229700a1bb3386325dff5e1da8abb011b670","1b18a0a020837e44ef5eb2ab72aa1a0fd02c89e5b92cde9efe727e72edee8065","a5fe7a2229dbfb460014b1253b62b9e278d60f54a60e33546272fd57377ea263","fc11bb0b3eb27a2e3a712a04d7dd53fde97080bb0bfbbc81ac180f427f195a4a","1fbe3c35951536c76f90437f6eb24a12a4c07741f1ed74e8b932d2b686288b56","2699f62eabc74331aeb6d2c1ec91a9e29991d9ecccd7eb6e29991eb47e0b31ce","defc0dcef5fd0c6b89445be995ffcc906723b3c88a8d1d05c0f83fc4ee14edea","cab447191f8cfefdadd893501181b1cffcf4f4770300e5f40a928da7c0fabe69","2bde21342f11358bd6d384b90928416f6100985a30e03c8bc649ca9f9567d67c"],"20000000","171297f6","5ecdcf8a",true]}`

		type jobParams struct {
			Id string
			Method string
			Params []interface{}
		}
		msg := &jobParams{}
		if err := json.Unmarshal([]byte(rawMessage), &msg); err != nil {
			log.Println("json unmarshal failed")
			return cs.sendError(req.Id, &ErrorReply{
				Code:    -1,
				Message: "subscribe failed unmashal json data",
			}, true)
		}
		log.Println("mining.pushJob params:",msg.Params)

		return cs.pushMessage("mining.notify", rawMessage)

	default:
		errReply := s.handleUnknownRPC(req)
		return cs.sendError(req.Id, errReply, true)
	}
}

func (cs *Session) sendResult(id *json.RawMessage, result interface{}) error {
	cs.Lock()
	defer cs.Unlock()
	message := JSONRpcResp{Id: id, Version: "2.0", Error: nil, Result: result}
	return cs.enc.Encode(&message)
}

func (cs *Session) pushMessage(method string, params interface{}) error {
	cs.Lock()
	defer cs.Unlock()

	fmt.Println("push Message-> method:", method, "params:", params)
	message := JSONPushMessage{Version: "2.0", Method: method, Params: params}
	return cs.enc.Encode(&message)
}

func (cs *Session) sendError(id *json.RawMessage, reply *ErrorReply, drop bool) error {
	cs.Lock()
	defer cs.Unlock()
	message := JSONRpcResp{Id: id, Version: "2.0", Error: reply}
	err := cs.enc.Encode(&message)
	if err != nil {
		return err
	}
	if drop {
		return fmt.Errorf("Server disconnect request")
	}
	return nil
}

func (s *StratumServer) setDeadline(conn *net.TCPConn) {
	conn.SetDeadline(time.Now().Add(s.timeout))
}

func (s *StratumServer) registerSession(cs *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	s.sessions[cs] = struct{}{}
}

func (s *StratumServer) removeSession(cs *Session) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	delete(s.sessions, cs)
}

func (s *StratumServer) isActive(cs *Session) bool {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	_, exist := s.sessions[cs]
	return exist
}

func (s *StratumServer) registerMiner(miner *Miner) {
	s.miners.Set(miner.id, miner)
}

func (s *StratumServer) removeMiner(id string) {
	s.miners.Remove(id)
}

func (s *StratumServer) currentBlockTemplate() *BlockTemplate {
	if t := s.blockTemplate.Load(); t != nil {
		return t.(*BlockTemplate)
	}
	return nil
}

func (s *StratumServer) checkUpstreams() {
	candidate := int32(0)
	backup := false

	for i, v := range s.upstreams {
		ok, err := v.Check()
		if err != nil {
			log.Printf("Upstream %v didn't pass check: %v", v.Name, err)
		}
		if ok && !backup {
			candidate = int32(i)
			backup = true
		}
	}

	if s.upstream != candidate {
		log.Printf("Switching to %v upstream", s.upstreams[candidate].Name)
		atomic.StoreInt32(&s.upstream, candidate)
	}
}

func (s *StratumServer) rpc() *rpc.RPCClient {
	i := atomic.LoadInt32(&s.upstream)
	return s.upstreams[i]
}

func (s *StratumServer) markSick() {
	atomic.AddInt64(&s.failsCount, 1)
}

func (s *StratumServer) markOk() {
	atomic.StoreInt64(&s.failsCount, 0)
}
