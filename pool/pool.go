package pool

type Config struct {
	Address                 string     `json:"address"`
	BypassAddressValidation bool       `json:"bypassAddressValidation"`
	BypassShareValidation   bool       `json:"bypassShareValidation"`
	Stratum                 Stratum    `json:"stratum"`
	BlockRefreshInterval    string     `json:"blockRefreshInterval"`
	UpstreamCheckInterval   string     `json:"upstreamCheckInterval"`
	Upstream                []Upstream `json:"upstream"`
	EstimationWindow        string     `json:"estimationWindow"`
	LuckWindow              string     `json:"luckWindow"`
	LargeLuckWindow         string     `json:"largeLuckWindow"`
	Threads                 int        `json:"threads"`
	NewrelicName            string     `json:"newrelicName"`
	NewrelicKey             string     `json:"newrelicKey"`
	NewrelicVerbose         bool       `json:"newrelicVerbose"`
	NewrelicEnabled         bool       `json:"newrelicEnabled"`

	Name                string `json:"name"`
	StateUpdateInterval string `json:"stateUpdateInterval"`
	HashrateExpiration  string `json:"hashrateExpiration"`
}

type Stratum struct {
	Timeout string `json:"timeout"`
	Ports   []Port `json:"listen"`
}

type Port struct {
	Difficulty int64  `json:"diff"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	MaxConn    int    `json:"maxConn"`

	AdaptiveDifficultyEnabled bool    `json:"adaptiveDifficultyEnabled"`
	StartDifficulty           int64   `json:"startDifficulty"`
	SubmitRate                float64 `json:"submitRate"`
}

type Upstream struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Timeout string `json:"timeout"`
}
