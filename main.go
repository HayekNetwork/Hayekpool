package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yvasiyarov/gorelic"

	"xcoin/HayekPool/api"
	"xcoin/HayekPool/payouts"
	"xcoin/HayekPool/storage"
	"xcoin/HayekPool/stratum"
)

var cfg stratum.SConfig
var backend *storage.RedisClient

func startStratum() {
	if cfg.Pool.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Pool.Threads)
		log.Printf("Running with %v threads", cfg.Pool.Threads)
	} else {
		n := runtime.NumCPU()
		runtime.GOMAXPROCS(n)
		log.Printf("Running with default %v threads", n)
	}

	s := stratum.NewStratum(&cfg, backend)
	s.Listen()
}

func startNewrelic() {
	// Run NewRelic
	if cfg.Pool.NewrelicEnabled {
		nr := gorelic.NewAgent()
		nr.Verbose = cfg.Pool.NewrelicVerbose
		nr.NewrelicLicense = cfg.Pool.NewrelicKey
		nr.NewrelicName = cfg.Pool.NewrelicName
		nr.Run()
	}
}

func startApi() {
	s := api.NewApiServer(&cfg.Api, backend)
	s.Start()
}

func startBlockUnlocker() {
	u := payouts.NewBlockUnlocker(&cfg.BlockUnlocker, backend)
	u.Start()
}

func startPayoutsProcessor() {
	u := payouts.NewPayoutsProcessor(&cfg.Payouts, backend)
	u.Start()
}

func readConfig(cfg *stratum.SConfig) {
	configFileName := "hayekconfig.json"
	if len(os.Args) > 1 {
		configFileName = os.Args[1]
	}
	configFileName, _ = filepath.Abs(configFileName)
	log.Printf("Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Fatal("File error: ", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err = jsonParser.Decode(&cfg); err != nil {
		log.Fatal("Config error: ", err.Error())
	}
}

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	readConfig(&cfg)

	if cfg.Coin == "" || strings.Contains(cfg.Coin, "?") {
		cfg.Coin = "hyk"
	}

	backend = storage.NewRedisClient(&cfg.Redis, cfg.Coin)
	if backend == nil {
		log.Printf("Failed to create backend !")
	}

	pong, err := backend.Check()
	if err != nil {
		log.Printf("Can't establish connection to backend: %v", err)
	} else {
		log.Printf("Backend check reply: %v", pong)
	}

	if cfg.Api.Enabled {
		go startApi()
	}
	if cfg.BlockUnlocker.Enabled {
		go startBlockUnlocker()
	}
	if cfg.Payouts.Enabled {
		go startPayoutsProcessor()
	}

	startNewrelic()
	startStratum()

	quit := make(chan bool)
	<-quit
}
