//   Copyright (C) 2018 ZVChain
//
//   This program is free software: you can redistribute it and/or modify
//   it under the terms of the GNU General Public License as published by
//   the Free Software Foundation, either version 3 of the License, or
//   (at your option) any later version.
//
//   This program is distributed in the hope that it will be useful,
//   but WITHOUT ANY WARRANTY; without even the implied warranty of
//   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//   GNU General Public License for more details.
//
//   You should have received a copy of the GNU General Public License
//   along with this program.  If not, see <https://www.gnu.org/licenses/>.

package logical

import (
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/consensus/base"
	"github.com/darren0718/zvchain/consensus/model"
	"github.com/darren0718/zvchain/middleware/time"
	"github.com/darren0718/zvchain/middleware/types"
)

func getCastExpireTime(base time.TimeStamp, deltaHeight uint64, castHeight uint64) time.TimeStamp {
	t := uint64(0)

	// When the cast height is 1, the expiration time is 5 times. In case the
	// node starts to be out of sync, the first proposed block expires prematurely,
	// causing the same node to propose the height 1 multiple times.
	if castHeight == 1 {
		t = 2
	}
	return base.AddSeconds(int64(t+deltaHeight) * int64(model.Param.MaxGroupCastTime))
}

func deltaHeightByTime(bh *types.BlockHeader, preBH *types.BlockHeader) uint64 {
	var (
		deltaHeightByTime uint64
	)
	if bh.Height == 1 {
		d := time.TSInstance.SinceSeconds(preBH.CurTime)
		deltaHeightByTime = uint64(d)/uint64(model.Param.MaxGroupCastTime) + 1
	} else {
		deltaHeightByTime = bh.Height - preBH.Height
	}
	return deltaHeightByTime
}

func expireTime(bh *types.BlockHeader, preBH *types.BlockHeader) time.TimeStamp {
	return getCastExpireTime(preBH.CurTime, deltaHeightByTime(bh, preBH), bh.Height)
}

func calcRandomHash(preBH *types.BlockHeader, height uint64) common.Hash {
	data := preBH.Random
	var hash common.Hash

	deltaHeight := height - preBH.Height
	for ; deltaHeight > 0; deltaHeight-- {
		hash = base.Data2CommonHash(data)
		data = hash.Bytes()
	}
	return hash
}
