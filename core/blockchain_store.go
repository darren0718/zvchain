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
	"fmt"
	"github.com/darren0718/zvchain/common/prque"
	"github.com/darren0718/zvchain/log"
	"github.com/darren0718/zvchain/middleware/notify"
	time2 "github.com/darren0718/zvchain/middleware/time"
	"github.com/darren0718/zvchain/monitor"
	"github.com/sirupsen/logrus"
	"sync/atomic"
	"time"

	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/middleware/types"
	"github.com/darren0718/zvchain/storage/account"
)

const TriesInMemory uint64 = types.EpochLength*4 + 20

var (
	maxTriesInMemory     = 300
	everyClearFromMemory = 1
	persistenceCount     = 4000
)

type newTopMessage struct {
	bh *types.BlockHeader
}

func (msg *newTopMessage) GetRaw() []byte {
	return nil
}

func (msg *newTopMessage) GetData() interface{} {
	return msg.bh
}

func (chain *FullBlockChain) getPruneHeights(cpHeight, minSize uint64) []*prque.Item {
	if cpHeight <= minSize {
		return nil
	}
	root, h := chain.triegc.Peek()
	if uint64(h) < cpHeight {
		return nil
	}
	backList := []*prque.Item{}
	cropList := []*prque.Item{}
	var count uint64 = 0
	temp := make(map[uint64]struct{})

	for !chain.triegc.Empty() {
		root, h = chain.triegc.Pop()
		if uint64(h) >= cpHeight {
			backList = append(backList, &prque.Item{root, h})
		} else {
			if _, ok := temp[uint64(h)]; !ok {
				temp[uint64(h)] = struct{}{}
				count++
			}
			if count <= minSize {
				backList = append(backList, &prque.Item{root, h})
			} else {
				cropList = append(cropList, &prque.Item{root, h})
			}
		}
	}
	if len(backList) > 0 {
		for _, v := range backList {
			chain.triegc.Push(v.Value, v.Priority)
		}
	}

	return cropList
}

func (chain *FullBlockChain) saveBlockState(b *types.Block, state *account.AccountDB) error {
	triedb := chain.stateCache.TrieDB()
	triedb.ResetNodeCache()
	begin := time.Now()
	defer func() {
		end := time.Now()
		cost := (end.UnixNano() - begin.UnixNano()) / 1e6
		if cost > 500 {
			log.CoreLogger.Debugf("save block state cost %v,height is %v", cost, b.Header.Height)
		}
	}()
	root, err := state.Commit(true)
	if err != nil {
		return fmt.Errorf("state commit error:%s", err.Error())
	}
	if chain.config.pruneMode && b.Header.Height > 0 {
		err = triedb.InsertStateDatasToSmallDb(root, chain.smallStateDb)
		if err != nil {
			return fmt.Errorf("insert full state nodes failed,err is %v", err)
		}
		triedb.Reference(root, common.Hash{}) // metadata reference to keep trie alive
		chain.triegc.Push(root, int64(b.Header.Height))
		cp := chain.latestCP.Load()
		limit := common.StorageSize(common.GlobalConf.GetInt(gc, "max_tries_memory", maxTriesInMemory) * 1024 * 1024)
		nodes, _ := triedb.Size()
		if nodes > limit {
			clear := common.StorageSize(common.GlobalConf.GetInt(gc, "clear_tries_memory", everyClearFromMemory) * 1024 * 1024)
			triedb.Cap(b.Header.Height, limit-clear)
		}
		if cp != nil {
			cropItems := chain.getPruneHeights(cp.(*types.BlockHeader).Height, TriesInMemory)
			if len(cropItems) > 0 {
				for i := len(cropItems) - 1; i >= 0; i-- {
					vl := cropItems[i]
					triedb.Dereference(uint64(vl.Priority), vl.Value.(common.Hash))
				}
				curCropMaxHeight := uint64(cropItems[0].Priority)
				triedb.StoreGcData(curCropMaxHeight, b.Header.Height, cp.(*types.BlockHeader).Height, uint64(len(cropItems)))
				persistentCount := common.GlobalConf.GetInt(gc, "persistence_count", persistenceCount)
				if triedb.CanPersistent(persistentCount) {
					bh := chain.queryBlockHeaderCeil(curCropMaxHeight + 1)
					if bh != nil {
						err = triedb.Commit(bh.Height, bh.StateTree, false)
						if err != nil {
							return fmt.Errorf("trie commit error:%s", err.Error())
						}
						triedb.ResetGcCount()
						err = chain.smallStateDb.StoreStatePersistentHeight(bh.Height)
						if err != nil {
							return fmt.Errorf("StoreTriePersistentHeight error:%s", err.Error())
						}
						go chain.DeleteSmallDbByHeight(bh.Height)
						log.CropLogger.Debugf("persistent height is %v,current height is %v,cp height is %v", bh.Height, b.Header.Height, cp.(*types.BlockHeader).Height)
					} else {
						log.CoreLogger.Warnf("persistent find ceil head is nil,height is %v", curCropMaxHeight)
					}
				}
			}
		}
	} else {
		err = triedb.Commit(b.Header.Height, root, false)
		if err != nil {
			return fmt.Errorf("trie commit error:%s", err.Error())
		}
	}
	return nil
}

func (chain *FullBlockChain) saveCurrentBlock(hash common.Hash) error {
	err := chain.blocks.AddKv(chain.batch, []byte(blockStatusKey), hash.Bytes())
	if err != nil {
		return err
	}
	return nil
}

func (chain *FullBlockChain) updateLatestBlock(state *account.AccountDB, header *types.BlockHeader) {
	chain.latestStateDB = state
	chain.latestBlock = header

	Logger.Debugf("updateLatestBlock success,height=%v,root hash is %x", header.Height, header.StateTree)
	//taslog.Flush()
}

func (chain *FullBlockChain) saveBlockHeader(hash common.Hash, dataBytes []byte) error {
	return chain.blocks.AddKv(chain.batch, hash.Bytes(), dataBytes)
}

func (chain *FullBlockChain) saveBlockHeight(height uint64, dataBytes []byte) error {
	return chain.blockHeight.AddKv(chain.batch, common.UInt64ToByte(height), dataBytes)
}

func (chain *FullBlockChain) saveBlockTxs(blockHash common.Hash, dataBytes []byte) error {
	return chain.txDb.AddKv(chain.batch, blockHash.Bytes(), dataBytes)
}

// commitBlock persist a block in a batch
func (chain *FullBlockChain) commitBlock(block *types.Block, ps *executePostState) (ok bool, err error) {
	traceLog := monitor.NewPerformTraceLogger("commitBlock", block.Header.Hash, block.Header.Height)
	traceLog.SetParent("addBlockOnChain")
	defer traceLog.Log("")

	bh := block.Header

	var (
		headerBytes []byte
		bodyBytes   []byte
	)
	//b := time.Now()
	headerBytes, err = types.MarshalBlockHeader(bh)
	//ps.ts.AddStat("MarshalBlockHeader", time.Since(b))
	if err != nil {
		Logger.Errorf("Fail to json Marshal, error:%s", err.Error())
		return
	}

	//b = time.Now()
	bodyBytes, err = encodeBlockTransactions(block)
	//ps.ts.AddStat("encodeBlockTransactions", time.Since(b))
	if err != nil {
		Logger.Errorf("encode block transaction error:%v", err)
		return
	}

	chain.rwLock.Lock()
	defer chain.rwLock.Unlock()
	if atomic.LoadInt32(&chain.running) == 1 {
		err = fmt.Errorf("in shutdown hook")
		return
	}
	defer chain.batch.Reset()

	// Commit state
	if err = chain.saveBlockState(block, ps.state); err != nil {
		return
	}
	// Save hash to block header key value pair
	if err = chain.saveBlockHeader(bh.Hash, headerBytes); err != nil {
		return
	}
	// Save height to block hash key value pair
	if err = chain.saveBlockHeight(bh.Height, bh.Hash.Bytes()); err != nil {
		return
	}
	// Save hash to transactions key value pair
	if err = chain.saveBlockTxs(bh.Hash, bodyBytes); err != nil {
		return
	}
	// Save hash to receipt key value pair
	if err = chain.transactionPool.SaveReceipts(bh.Hash, ps.receipts); err != nil {
		return
	}
	// Save current block
	if err = chain.saveCurrentBlock(bh.Hash); err != nil {
		return
	}
	// Batch write
	if err = chain.batch.Write(); err != nil {
		return
	}
	//ps.ts.AddStat("batch.Write", time.Since(b))

	chain.updateLatestBlock(ps.state, bh)

	rmTxLog := monitor.NewPerformTraceLogger("RemoveFromPool", block.Header.Hash, block.Header.Height)
	rmTxLog.SetParent("commitBlock")
	defer rmTxLog.Log("")

	// If the block is successfully submitted, the transaction
	// corresponding to the transaction pool should be deleted
	removeTxs := make([]common.Hash, 0)
	if len(ps.txs) > 0 {
		removeTxs = append(removeTxs, ps.txs.txsHashes()...)
	}
	// Remove eviction transactions from the transaction pool
	if ps.evictedTxs != nil {
		if len(ps.evictedTxs) > 0 {
			Logger.Infof("block commit remove evictedTxs: %v, block height: %d", ps.evictedTxs, bh.Height)
		}
		removeTxs = append(removeTxs, ps.evictedTxs...)
	}
	chain.transactionPool.RemoveFromPool(removeTxs)
	ok = true
	return
}

func (chain *FullBlockChain) resetTop(block *types.BlockHeader) error {
	if !chain.isAdjusting {
		chain.isAdjusting = true
		defer func() {
			chain.isAdjusting = false
		}()
	}

	// Add read and write locks, block reading at this time
	chain.rwLock.Lock()
	defer chain.rwLock.Unlock()

	if atomic.LoadInt32(&chain.running) == 1 {
		return fmt.Errorf("in shutdown hook")
	}

	if nil == block {
		return fmt.Errorf("block is nil")
	}
	traceLog := monitor.NewPerformTraceLogger("resetTop", block.Hash, block.Height)
	traceLog.SetParent("addBlockOnChain")
	defer traceLog.Log("")
	if block.Hash == chain.latestBlock.Hash {
		return nil
	}
	Logger.Debugf("reset top hash:%s height:%d ", block.Hash.Hex(), block.Height)

	var err error

	defer chain.batch.Reset()

	curr := chain.getLatestBlock()
	recoverTxs := make([]*types.Transaction, 0)
	delReceipts := make([]common.Hash, 0)
	removeBlocks := make([]*types.BlockHeader, 0)
	removeRoots := make([]common.Hash, 0)
	for curr.Hash != block.Hash {
		// Delete the old block header
		if err = chain.saveBlockHeader(curr.Hash, nil); err != nil {
			return err
		}
		// Delete the old block height
		if err = chain.saveBlockHeight(curr.Height, nil); err != nil {
			return err
		}
		// Delete the old block's transactions
		if err = chain.saveBlockTxs(curr.Hash, nil); err != nil {
			return err
		}
		rawTxs := chain.queryBlockTransactionsAll(curr.Hash)
		for _, rawTx := range rawTxs {
			tHash := rawTx.GenHash()
			recoverTxs = append(recoverTxs, types.NewTransaction(rawTx, tHash))
			delReceipts = append(delReceipts, tHash)
		}
		removeRoots = append(removeRoots, curr.Hash)
		chain.removeTopBlock(curr.Hash)
		removeBlocks = append(removeBlocks, curr)
		Logger.Debugf("remove block %v", curr.Hash.Hex())
		if curr.PreHash == block.Hash {
			break
		}
		curr = chain.queryBlockHeaderByHash(curr.PreHash)
	}
	// Delete receipts corresponding to the transactions in the discard block
	if err = chain.transactionPool.DeleteReceipts(delReceipts); err != nil {
		return err
	}
	// Reset the current block
	if err = chain.saveCurrentBlock(block.Hash); err != nil {
		return err
	}
	state, err := account.NewAccountDB(block.StateTree, chain.stateCache)
	if err != nil {
		return err
	}
	if err = chain.batch.Write(); err != nil {
		return err
	}
	if chain.config.pruneMode {
		chain.DeleteSmallDbDatasByRoots(removeRoots)
	}

	chain.updateLatestBlock(state, block)

	chain.transactionPool.BackToPool(recoverTxs)
	log.ELKLogger.WithFields(logrus.Fields{
		"removedHeight": len(removeBlocks),
		"now":           time2.TSInstance.Now().UTC(),
		"logType":       "resetTop",
		"version":       common.GzvVersion,
	}).Info("resetTop")
	for _, b := range removeBlocks {
		GroupManagerImpl.OnBlockRemove(b)
	}
	// invalidate latest cp cache
	chain.latestCP = atomic.Value{}

	// Notify reset top message
	notify.BUS.Publish(notify.NewTopBlock, &newTopMessage{bh: block})

	return nil
}

// removeOrphan remove the orphan block
func (chain *FullBlockChain) removeOrphan(block *types.Block) error {

	// Add read and write locks, block reading at this time
	chain.rwLock.Lock()
	defer chain.rwLock.Unlock()

	if nil == block {
		return nil
	}
	hash := block.Header.Hash
	height := block.Header.Height
	Logger.Debugf("remove hash:%s height:%d ", hash.Hex(), height)

	var err error
	defer chain.batch.Reset()

	if err = chain.saveBlockHeader(hash, nil); err != nil {
		return err
	}
	if err = chain.saveBlockHeight(height, nil); err != nil {
		return err
	}
	if err = chain.saveBlockTxs(hash, nil); err != nil {
		return err
	}
	txs := chain.queryBlockTransactionsAll(hash)
	if txs != nil {
		txHashs := make([]common.Hash, len(txs))
		for i, tx := range txs {
			txHashs[i] = tx.GenHash()
		}
		if err = chain.transactionPool.DeleteReceipts(txHashs); err != nil {
			return err
		}
	}

	if err = chain.batch.Write(); err != nil {
		return err
	}
	chain.removeTopBlock(hash)
	return nil
}

func (chain *FullBlockChain) loadCurrentBlock() *types.BlockHeader {
	bs, err := chain.blocks.Get([]byte(blockStatusKey))
	if err != nil {
		return nil
	}
	hash := common.BytesToHash(bs)
	return chain.queryBlockHeaderByHash(hash)
}

func (chain *FullBlockChain) hasBlock(hash common.Hash) bool {
	if ok, _ := chain.blocks.Has(hash.Bytes()); ok {
		return ok
	}
	return false
	//pre := gchain.queryBlockHeaderByHash(bh.PreHash)
	//return pre != nil
}

func (chain *FullBlockChain) hasHeight(h uint64) bool {
	if ok, _ := chain.blockHeight.Has(common.UInt64ToByte(h)); ok {
		return ok
	}
	return false
}

func (chain *FullBlockChain) queryBlockHash(height uint64) *common.Hash {
	result, _ := chain.blockHeight.Get(common.UInt64ToByte(height))
	if result != nil {
		hash := common.BytesToHash(result)
		return &hash
	}
	return nil
}

func (chain *FullBlockChain) queryBlockHeaderCeil(height uint64) *types.BlockHeader {
	hash := chain.queryBlockHashCeil(height)
	if hash != nil {
		return chain.queryBlockHeaderByHash(*hash)
	}
	return nil
}

func (chain *FullBlockChain) queryBlockHashCeil(height uint64) *common.Hash {
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()
	if iter.Seek(common.UInt64ToByte(height)) {
		hash := common.BytesToHash(iter.Value())
		return &hash
	}
	return nil
}

func (chain *FullBlockChain) queryBlockHeaderByHeightFloor(height uint64) *types.BlockHeader {
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()
	if iter.Seek(common.UInt64ToByte(height)) {
		realHeight := common.ByteToUInt64(iter.Key())
		if realHeight == height {
			hash := common.BytesToHash(iter.Value())
			bh := chain.queryBlockHeaderByHash(hash)
			if bh == nil {
				Logger.Errorf("data error:height %v, hash %v", height, hash.Hex())
				return nil
			}
			if bh.Height != height {
				Logger.Errorf("key height not equal to value height:keyHeight=%v, valueHeight=%v", realHeight, bh.Height)
				return nil
			}
			return bh
		}
	}
	if iter.Prev() {
		hash := common.BytesToHash(iter.Value())
		return chain.queryBlockHeaderByHash(hash)
	}
	return nil
}

func (chain *FullBlockChain) queryBlockBodyBytes(hash common.Hash) []byte {
	bs, err := chain.txDb.Get(hash.Bytes())
	if err != nil {
		Logger.Errorf("get txDb err:%v, key:%v", err.Error(), hash.Hex())
		return nil
	}
	return bs
}

func (chain *FullBlockChain) queryBlockTransactionsAll(hash common.Hash) []*types.RawTransaction {
	bs := chain.queryBlockBodyBytes(hash)
	if bs == nil {
		return nil
	}
	txs, err := decodeBlockTransactions(bs)
	if err != nil {
		Logger.Errorf("decode transactions err:%v, key:%v", err.Error(), hash.Hex())
		return nil
	}
	return txs
}

func (chain *FullBlockChain) batchGetBlocksAfterHeight(h uint64, limit int) []*types.Block {
	blocks := make([]*types.Block, 0)
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()

	// No higher block after the specified block height
	if !iter.Seek(common.UInt64ToByte(h)) {
		return blocks
	}
	cnt := 0
	for cnt < limit {
		hash := common.BytesToHash(iter.Value())
		b := chain.queryBlockByHash(hash)
		if b == nil {
			break
		}
		blocks = append(blocks, b)
		if !iter.Next() {
			break
		}
		cnt++
	}
	return blocks
}

// scanBlockHeightsInRange returns the heights of block in the given height range. the block with startHeight and endHeight
// will be included
func (chain *FullBlockChain) scanBlockHeightsInRange(startHeight uint64, endHeight uint64) []uint64 {
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()
	// No higher block after the specified block height
	if !iter.Seek(common.UInt64ToByte(startHeight)) {
		return []uint64{}
	}

	hs := make([]uint64, 0)
	for {
		height := common.ByteToUInt64(iter.Key())
		if height > endHeight {
			break
		}
		hs = append(hs, height)
		if !iter.Next() {
			break
		}
	}
	return hs
}

// countBlocksInRange returns the count of blocks in the given height range. the block with startHeight and endHeight
// will be included
func (chain *FullBlockChain) countBlocksInRange(startHeight uint64, endHeight uint64) (count uint64) {
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()
	// No higher block after the specified block height
	if !iter.Seek(common.UInt64ToByte(startHeight)) {
		return
	}
	for {
		height := common.ByteToUInt64(iter.Key())
		if height > endHeight {
			break
		}
		count++
		if !iter.Next() {
			break
		}
	}
	return
}

func (chain *FullBlockChain) queryBlockHeaderByHeight(height uint64) *types.BlockHeader {
	hash := chain.queryBlockHash(height)
	if hash != nil {
		return chain.queryBlockHeaderByHash(*hash)
	}
	return nil
}

func (chain *FullBlockChain) queryBlockByHash(hash common.Hash) *types.Block {
	bh := chain.queryBlockHeaderByHash(hash)
	if bh == nil {
		return nil
	}

	txs := chain.queryBlockTransactionsAll(hash)
	b := &types.Block{
		Header:       bh,
		Transactions: txs,
	}
	return b
}

func (chain *FullBlockChain) queryBlockHeaderBytes(hash common.Hash) []byte {
	result, _ := chain.blocks.Get(hash.Bytes())
	return result
}

func (chain *FullBlockChain) queryBlockHeaderByHash(hash common.Hash) *types.BlockHeader {
	bs := chain.queryBlockHeaderBytes(hash)
	if bs != nil {
		block, err := types.UnMarshalBlockHeader(bs)
		if err != nil {
			fmt.Println(err)
			return nil
		}
		return block
	}
	return nil
}

func (chain *FullBlockChain) addTopBlock(b *types.Block) {
	chain.topRawBlocks.Add(b.Header.Hash, b)
}

func (chain *FullBlockChain) removeTopBlock(hash common.Hash) {
	chain.topRawBlocks.Remove(hash)
}

func (chain *FullBlockChain) getTopBlockByHash(hash common.Hash) *types.Block {
	if v, ok := chain.topRawBlocks.Get(hash); ok {
		return v.(*types.Block)
	}
	return nil
}

func (chain *FullBlockChain) getTopBlockByHeight(height uint64) *types.Block {
	if chain.topRawBlocks.Len() == 0 {
		return nil
	}
	for _, k := range chain.topRawBlocks.Keys() {
		b := chain.getTopBlockByHash(k.(common.Hash))
		if b != nil && b.Header.Height == height {
			return b
		}
	}
	return nil
}

func (chain *FullBlockChain) queryBlockTransactionsOptional(txIdx int, height uint64) *types.RawTransaction {
	bh := chain.queryBlockHeaderByHeight(height)
	if bh == nil {
		return nil
	}
	bs, err := chain.txDb.Get(bh.Hash.Bytes())
	if err != nil {
		Logger.Errorf("queryBlockTransactionsOptional get txDb err:%v, key:%v", err.Error(), bh.Hash.Hex())
		return nil
	}
	tx, err := decodeTransaction(txIdx, bs)
	if tx != nil {
		return tx
	}
	return nil
}

// batchGetBlocksBetween query blocks of the height range [start, end)
func (chain *FullBlockChain) batchGetBlocksBetween(begin, end uint64) []*types.Block {
	blocks := make([]*types.Block, 0)
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()

	// No higher block after the specified block height
	if !iter.Seek(common.UInt64ToByte(begin)) {
		return blocks
	}
	for {
		height := common.ByteToUInt64(iter.Key())
		if height >= end {
			break
		}
		hash := common.BytesToHash(iter.Value())
		b := chain.queryBlockByHash(hash)
		if b == nil {
			break
		}

		blocks = append(blocks, b)
		if !iter.Next() {
			break
		}
	}
	return blocks
}

// batchGetBlockHeadersBetween query blocks of the height range [start, end)
func (chain *FullBlockChain) batchGetBlockHeadersBetween(begin, end uint64) []*types.BlockHeader {
	blocks := make([]*types.BlockHeader, 0)
	iter := chain.blockHeight.NewIterator()
	defer iter.Release()

	// No higher block after the specified block height
	if !iter.Seek(common.UInt64ToByte(begin)) {
		return blocks
	}
	for {
		height := common.ByteToUInt64(iter.Key())
		if height >= end {
			break
		}
		hash := common.BytesToHash(iter.Value())
		b := chain.queryBlockHeaderByHash(hash)
		if b == nil {
			break
		}

		blocks = append(blocks, b)
		if !iter.Next() {
			break
		}
	}
	return blocks
}
