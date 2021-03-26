package storage

import (
	"flag"
	"os"
	"reflect"
	"strconv"
	"testing"

	"gopkg.in/redis.v3"
)

const prefix = "test"

var (
	// 127.0.0.1:6379
	t_flagRedisHost = flag.String("test-redis-host", "", "set redis host")

	t_r *RedisClient
)

func TestMain(m *testing.M) {
	flag.Parse()

	if *t_flagRedisHost != "" {
		t_r = NewRedisClient(&Config{Endpoint: "127.0.0.1:6379"}, prefix)
	}

	tResetRedis()
	c := m.Run()
	tResetRedis()
	os.Exit(c)
}

func TestWriteShareCheckExist(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	exist, _ := t_r.WriteShare("x", "x", []string{"0x0", "0x0", "0x0"}, 10, 1008, 0)
	if exist {
		t.Error("PoW must not exist")
	}
	exist, _ = t_r.WriteShare("x", "x", []string{"0x0", "0x1", "0x0"}, 10, 1008, 0)
	if exist {
		t.Error("PoW must not exist")
	}
	exist, _ = t_r.WriteShare("x", "x", []string{"0x0", "0x0", "0x1"}, 100, 1010, 0)
	if exist {
		t.Error("PoW must not exist")
	}
	exist, _ = t_r.WriteShare("z", "x", []string{"0x0", "0x0", "0x1"}, 100, 1016, 0)
	if !exist {
		t.Error("PoW must exist")
	}
	exist, _ = t_r.WriteShare("x", "x", []string{"0x0", "0x0", "0x1"}, 100, 1025, 0)
	if exist {
		t.Error("PoW must not exist")
	}
}

func TestGetPayees(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	n := 256
	for i := 0; i < n; i++ {
		t_r.client.HSet(t_r.formatKey("miners", strconv.Itoa(i)), "balance", strconv.Itoa(i))
	}

	var payees []string
	payees, _ = t_r.GetPayees()
	if len(payees) != n {
		t.Error("Must return all payees")
	}
	m := make(map[string]struct{})
	for _, v := range payees {
		m[v] = struct{}{}
	}
	if len(m) != n {
		t.Error("Must be unique list")
	}
}

func TestGetBalance(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.client.HSet(t_r.formatKey("miners:x"), "balance", "750")

	v, _ := t_r.GetBalance("x")
	if v != 750 {
		t.Error("Must return balance")
	}

	v, err := t_r.GetBalance("z")
	if v != 0 {
		t.Error("Must return 0 if account does not exist")
	}
	if err != nil {
		t.Error("Must not return error if account does not exist")
	}
}

func TestLockPayouts(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.LockPayouts("x", 1000)
	v := t_r.client.Get("test:payments:lock").Val()
	if v != "x:1000" {
		t.Errorf("Invalid lock amount: %v", v)
	}

	err := t_r.LockPayouts("x", 100)
	if err == nil {
		t.Errorf("Must not overwrite lock")
	}
}

func TestUnlockPayouts(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.client.Set(t_r.formatKey("payments:lock"), "x:1000", 0)

	t_r.UnlockPayouts()
	err := t_r.client.Get(t_r.formatKey("payments:lock")).Err()
	if err != redis.Nil {
		t.Errorf("Must release lock")
	}
}

func TestIsPayoutsLocked(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.LockPayouts("x", 1000)
	if locked, _ := t_r.IsPayoutsLocked(); !locked {
		t.Errorf("Payouts must be locked")
	}
}

func TestUpdateBalance(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.client.HMSetMap(
		t_r.formatKey("miners:x"),
		map[string]string{"paid": "50", "balance": "1000"},
	)
	t_r.client.HMSetMap(
		t_r.formatKey("finances"),
		map[string]string{"paid": "500", "balance": "10000"},
	)

	amount := int64(250)
	t_r.UpdateBalance("x", amount)
	result := t_r.client.HGetAllMap(t_r.formatKey("miners:x")).Val()
	if result["pending"] != "250" {
		t.Error("Must set pending amount")
	}
	if result["balance"] != "750" {
		t.Error("Must deduct balance")
	}
	if result["paid"] != "50" {
		t.Error("Must not touch paid")
	}

	result = t_r.client.HGetAllMap(t_r.formatKey("finances")).Val()
	if result["pending"] != "250" {
		t.Error("Must set pool pending amount")
	}
	if result["balance"] != "9750" {
		t.Error("Must deduct pool balance")
	}
	if result["paid"] != "500" {
		t.Error("Must not touch pool paid")
	}

	rank := t_r.client.ZRank(t_r.formatKey("payments:pending"), join("x", amount)).Val()
	if rank != 0 {
		t.Error("Must add pending payment")
	}
}

func TestRollbackBalance(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.client.HMSetMap(
		t_r.formatKey("miners:x"),
		map[string]string{"paid": "100", "balance": "750", "pending": "250"},
	)
	t_r.client.HMSetMap(
		t_r.formatKey("finances"),
		map[string]string{"paid": "500", "balance": "10000", "pending": "250"},
	)
	t_r.client.ZAdd(t_r.formatKey("payments:pending"), redis.Z{Score: 1, Member: "xx"})

	amount := int64(250)
	t_r.RollbackBalance("x", amount)
	result := t_r.client.HGetAllMap(t_r.formatKey("miners:x")).Val()
	if result["paid"] != "100" {
		t.Error("Must not touch paid")
	}
	if result["balance"] != "1000" {
		t.Error("Must increase balance")
	}
	if result["pending"] != "0" {
		t.Error("Must deduct pending")
	}

	result = t_r.client.HGetAllMap(t_r.formatKey("finances")).Val()
	if result["paid"] != "500" {
		t.Error("Must not touch pool paid")
	}
	if result["balance"] != "10250" {
		t.Error("Must increase pool balance")
	}
	if result["pending"] != "0" {
		t.Error("Must deduct pool pending")
	}

	err := t_r.client.ZRank(t_r.formatKey("payments:pending"), join("x", amount)).Err()
	if err != redis.Nil {
		t.Errorf("Must remove pending payment")
	}
}

func TestWritePayment(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.client.HMSetMap(
		t_r.formatKey("miners:x"),
		map[string]string{"paid": "50", "balance": "1000", "pending": "250"},
	)
	t_r.client.HMSetMap(
		t_r.formatKey("finances"),
		map[string]string{"paid": "500", "balance": "10000", "pending": "250"},
	)

	amount := int64(250)
	t_r.WritePayment("x", "0x0", amount)
	result := t_r.client.HGetAllMap(t_r.formatKey("miners:x")).Val()
	if result["pending"] != "0" {
		t.Error("Must unset pending amount")
	}
	if result["balance"] != "1000" {
		t.Error("Must not touch balance")
	}
	if result["paid"] != "300" {
		t.Error("Must increase paid")
	}

	result = t_r.client.HGetAllMap(t_r.formatKey("finances")).Val()
	if result["pending"] != "0" {
		t.Error("Must deduct pool pending amount")
	}
	if result["balance"] != "10000" {
		t.Error("Must not touch pool balance")
	}
	if result["paid"] != "750" {
		t.Error("Must increase pool paid")
	}

	err := t_r.client.Get(t_r.formatKey("payments:lock")).Err()
	if err != redis.Nil {
		t.Errorf("Must release lock")
	}

	err = t_r.client.ZRank(t_r.formatKey("payments:pending"), join("x", amount)).Err()
	if err != redis.Nil {
		t.Error("Must remove pending payment")
	}
	err = t_r.client.ZRank(t_r.formatKey("payments:all"), join("0x0", "x", amount)).Err()
	if err == redis.Nil {
		t.Error("Must add payment to set")
	}
	err = t_r.client.ZRank(t_r.formatKey("payments:x"), join("0x0", amount)).Err()
	if err == redis.Nil {
		t.Error("Must add payment to set")
	}
}

func TestGetPendingPayments(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	t_r.client.HMSetMap(
		t_r.formatKey("miners:x"),
		map[string]string{"paid": "100", "balance": "750", "pending": "250"},
	)

	amount := int64(1000)
	t_r.UpdateBalance("x", amount)
	pending := t_r.GetPendingPayments()

	if len(pending) != 1 {
		t.Error("Must return pending payment")
	}
	if pending[0].Amount != amount {
		t.Error("Must have corrent amount")
	}
	if pending[0].Address != "x" {
		t.Error("Must have corrent account")
	}
	if pending[0].Timestamp <= 0 {
		t.Error("Must have timestamp")
	}
}

func TestCollectLuckStats(t *testing.T) {
	if t_r == nil {
		t.Skip("no redis")
	}

	tResetRedis()

	members := []redis.Z{
		redis.Z{Score: 0, Member: "1:0:0x0:0x0:0:100:100:0"},
	}
	t_r.client.ZAdd(t_r.formatKey("blocks:immature"), members...)
	members = []redis.Z{
		redis.Z{Score: 1, Member: "1:0:0x2:0x0:0:50:100:0"},
		redis.Z{Score: 2, Member: "0:1:0x1:0x0:0:100:100:0"},
		redis.Z{Score: 3, Member: "0:0:0x3:0x0:0:200:100:0"},
	}
	t_r.client.ZAdd(t_r.formatKey("blocks:matured"), members...)

	stats, _ := t_r.CollectLuckStats([]int{1, 2, 5, 10})
	expectedStats := map[string]interface{}{
		"1": map[string]float64{
			"luck": 1, "uncleRate": 1, "orphanRate": 0,
		},
		"2": map[string]float64{
			"luck": 0.75, "uncleRate": 0.5, "orphanRate": 0,
		},
		"4": map[string]float64{
			"luck": 1.125, "uncleRate": 0.5, "orphanRate": 0.25,
		},
	}

	if !reflect.DeepEqual(stats, expectedStats) {
		t.Error("Stats != expected stats")
	}
}

func tResetRedis() {
	if t_r == nil {
		return
	}

	keys := t_r.client.Keys(t_r.prefix + ":*").Val()
	for _, k := range keys {
		t_r.client.Del(k)
	}
}
