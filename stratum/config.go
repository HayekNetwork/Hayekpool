package stratum

import (
	"xcoin/HayekPool/api"
	"xcoin/HayekPool/payouts"
	"xcoin/HayekPool/policy"
	"xcoin/HayekPool/pool"
	"xcoin/HayekPool/storage"
)

type SConfig struct {
	Api           api.ApiConfig          `json:"api"`
	BlockUnlocker payouts.UnlockerConfig `json:"unlocker"`
	Payouts       payouts.PayoutsConfig  `json:"payouts"`
	Policy        policy.Config          `json:"policy"`
	Pool          pool.Config            `json:"pool"`
	Redis         storage.Config         `json:"redis"`
	Coin          string                 `json:"coin"`
}
