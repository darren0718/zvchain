package core

import (
	"testing"
)

func TestRewardManager_CalculateGasFeeVerifyRewards(t *testing.T) {
	rm := RewardManager{}
	gasFee := uint64(100)
	rewards := rm.CalculateGasFeeVerifyRewards(gasFee)
	correctRewards := gasFee * gasFeeVerifyRewardsWeight / gasFeeTotalRewardsWeight
	if rewards != correctRewards {
		t.Errorf("CalculateGasFeeVerifyRewards: rewards error, wanted: %d, got: %d",
			correctRewards, rewards)
	}
}

func TestRewardManager_CalculateGasFeeCastorRewards(t *testing.T) {
	rm := RewardManager{}
	gasFee := uint64(100)
	rewards := rm.CalculateGasFeeCastorRewards(gasFee)
	correctRewards := gasFee * gasFeeCastorRewardsWeight / gasFeeTotalRewardsWeight
	if rewards != correctRewards {
		t.Errorf("CalculateGasFeeVerifyRewards: rewards error, wanted: %d, got: %d",
			correctRewards, rewards)
	}
}

func TestRewardManager_Rewards(t *testing.T) {
	rm := newRewardManager()
	for i := uint64(0); i < 150000000; i++ {
		rm.reduceBlockRewards(i)
		blockRewards := rm.blockRewards(i)
		userNodeRewards := rm.userNodesRewards(i)
		correctUserNodeRewards := blockRewards * userNodeWeight / totalNodeWeight
		if userNodeRewards != correctUserNodeRewards {
			t.Errorf("userNodesRewards: rewards error, wanted: %d, got: %d",
				correctUserNodeRewards, userNodeRewards)
		}
		daemonNodeWeight := initialDaemonNodeWeight + i/adjustWeightPeriod*adjustWeight
		daemonNodeRewards := rm.daemonNodesRewards(i)
		correctDaemonNodeRewards := blockRewards * daemonNodeWeight / totalNodeWeight
		if daemonNodeRewards != correctDaemonNodeRewards {
			t.Errorf("daemonNodesRewards: rewards error, wanted: %d, got: %d",
				correctDaemonNodeRewards, userNodeRewards)
		}
		minerNodeRewards := rm.minerNodesRewards(i)
		minerNodeWeight := initialMinerNodeWeight - i/adjustWeightPeriod*adjustWeight
		correctMinerNodeRewards := blockRewards * minerNodeWeight / totalNodeWeight
		if minerNodeRewards != correctMinerNodeRewards {
			t.Errorf("minerNodesRewards: rewards error, wanted: %d, got: %d",
				correctMinerNodeRewards, minerNodeRewards)
		}
		castorRewards := rm.CalculateCastorRewards(i)
		correctCastorRewards := minerNodeRewards * castorRewardsWeight / totalRewardsWeight
		if castorRewards != correctCastorRewards {
			t.Errorf("CalculateCastorRewards: rewards error, wanted: %d, got: %d",
				correctCastorRewards, castorRewards)
		}
		packedRewards := rm.CalculatePackedRewards(i)
		correctPackedRewards := minerNodeRewards * packedRewardsWeight / totalRewardsWeight
		if packedRewards != correctPackedRewards {
			t.Errorf("CalculatePackedRewards: rewards error, wanted: %d, got: %d",
				correctPackedRewards, packedRewards)
		}
		verifyRewards := rm.CalculateVerifyRewards(i)
		correctVerifyRewards := minerNodeRewards * verifyRewardsWeight / totalRewardsWeight
		if verifyRewards != correctVerifyRewards {
			t.Errorf("CalculateVerifyRewards: rewards error, wanted: %d, got: %d",
				correctVerifyRewards, verifyRewards)
		}
	}
}
