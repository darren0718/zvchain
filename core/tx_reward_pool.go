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

package core

import (
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/middleware/types"
	lru "github.com/hashicorp/golang-lru"
)

type rewardPool struct {
	bm             *rewardManager
	pool           *lru.Cache // Is an LRU cache that stores the mapping of transaction hashes to transaction pointer
	blockHashIndex *lru.Cache // Is an LRU cache that stores the mapping of block hashes to slice of transaction pointer
}

func newRewardPool(pm *rewardManager, size int) *rewardPool {
	return &rewardPool{
		pool:           common.MustNewLRUCache(size * 10),
		blockHashIndex: common.MustNewLRUCache(size),
		bm:             pm,
	}
}

func (bp *rewardPool) add(tx *types.Transaction) bool {
	if bp.pool.Contains(tx.Hash) {
		return false
	}
	bp.pool.Add(tx.Hash, tx)
	blockHash := parseRewardBlockHash(tx)

	var txs []*types.Transaction
	if v, ok := bp.blockHashIndex.Get(blockHash); ok {
		txs = v.([]*types.Transaction)
	} else {
		txs = make([]*types.Transaction, 0)
	}
	txs = append(txs, tx)
	bp.blockHashIndex.Add(blockHash, txs)
	return true
}

func (bp *rewardPool) remove(txHash common.Hash) {
	tx, _ := bp.pool.Get(txHash)
	if tx != nil {
		bp.pool.Remove(txHash)
		bhash := parseRewardBlockHash(tx.(*types.Transaction))
		bp.removeByBlockHash(bhash)
	}
}

func (bp *rewardPool) removeByBlockHash(blockHash common.Hash) int {
	txs, _ := bp.blockHashIndex.Get(blockHash)
	cnt := 0
	if txs != nil {
		for _, trans := range txs.([]*types.Transaction) {
			bp.pool.Remove(trans.Hash)
			cnt++
		}
		bp.blockHashIndex.Remove(blockHash)
	}
	return cnt
}

func (bp *rewardPool) get(hash common.Hash) *types.Transaction {
	if v, ok := bp.pool.Get(hash); ok {
		return v.(*types.Transaction)
	}
	return nil
}

func (bp *rewardPool) len() int {
	return bp.pool.Len()
}

func (bp *rewardPool) contains(hash common.Hash) bool {
	return bp.pool.Contains(hash)
}

func (bp *rewardPool) hasReward(blockHashByte []byte) bool {
	return bp.bm.blockHasRewardTransaction(blockHashByte)
}

func (bp *rewardPool) forEachByBlock(f func(blockHash common.Hash, txs []*types.Transaction) bool) {
	for _, k := range bp.blockHashIndex.Keys() {
		v, _ := bp.blockHashIndex.Peek(k)
		if v != nil {
			if !f(k.(common.Hash), v.([]*types.Transaction)) {
				break
			}
		}
	}
}
