package payouts

import (
	"math"
	"math/big"

	"xcoin/HayekPool/util"
)

// QKC减产规范
//
// 参数名称             参数数值  参数单位
// 出块时间（块/秒）    30        秒
// 每分钟出块数         2         块数
// 每小时出块数         120       块数
// 每天出块数           2880      块数
// 每块产量             1000      个
// 每天产量             2880000   个
// 减产周期（块）       28800     块
// 相当于减产周期（天） 10         天
// 每个周期减产系数     0.7
// 首个周期产量        28,800,000 个

// 减产时间截至第270天，即777,600块高后，此后不再减产，即每块的产出不变

// 奖励的表格, 必须大于27次减半的长度
var qkc_RewardArray [100]*big.Int

// 根据区块高度获取对应区块的奖励
// 用于替代 getConstReward 函数
func GetConstReward_qkc(height int64) *big.Int {
	const (
		firstReward = 1000 // 第一次奖励(单位不是wei)

		reduceRate             = 0.5        // 减半系数
		reduceRewardHeightStep = 28800      // 每次减半的高度间隔
		reduceRewardHeightMax  = 777600 - 1 // 最后一次减半的最大高度
	)

	// 减产时间截至第270天，即777,600块高后，此后不再减产，即每块的产出不变
	if height > reduceRewardHeightMax {
		height = reduceRewardHeightMax
	}

	// 根据高度计算对应第几次减半
	xIndex := height / reduceRewardHeightStep

	// 重新生成表格
	//if qkc_RewardArray[xIndex] == nil {
	// 当前的减半系数(基于第一个块奖励)
	// float64 有 12-13 位的十进制精度, 对于 27 次减半的精度足够
	xRate := math.Pow(reduceRate, float64(xIndex))

	// 当前的奖励
	xReward := new(big.Float).Mul(big.NewFloat(firstReward), big.NewFloat(xRate))

	// 转化位Wei单位
	xRewardWei := xReward.Mul(xReward, new(big.Float).SetInt(util.Ether))

	// 转化为 big.Int 类型, 并缓存到表格中
	v, _ := xRewardWei.Int(nil)

	// 可能出现叔块, 而叔块的计算方式是不同的, 因此暂时关闭缓存
	// qkc_RewardArray[xIndex] = v
	return v
	//}

	// 查表格(必须 clone 一个新对象, 避免缓存被外部函数破坏)
	//return new(big.Int).SetBits(qkc_RewardArray[xIndex].Bits())
}

func wei2Ether(wei *big.Int) (coinEther float64) {
	coinEther, _ = new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		new(big.Float).SetInt(util.Ether),
	).Float64()
	return
}
