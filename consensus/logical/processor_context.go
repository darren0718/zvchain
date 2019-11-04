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
	"math"
	"sync"
	"time"

	"github.com/darren0718/zvchain/consensus/groupsig"
	"gopkg.in/fatih/set.v0"

	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/consensus/model"
	time2 "github.com/darren0718/zvchain/middleware/time"
	"github.com/darren0718/zvchain/middleware/types"
	lru "github.com/hashicorp/golang-lru"
)

type castedBlock struct {
	height  uint64
	preHash common.Hash
}
type verifyMsgCache struct {
	verifyMsgs []*model.ConsensusVerifyMessage
	expire     time.Time
	lock       sync.RWMutex
}

type proposedBlock struct {
	lock             sync.RWMutex
	block            *types.Block
	maxResponseCount int
	requestedMember  set.Interface
}

func newProposedBlock(b *types.Block, count int) *proposedBlock {
	return &proposedBlock{
		block:            b,
		requestedMember:  set.New(set.ThreadSafe),
		maxResponseCount: count,
	}
}

func (p *proposedBlock) containsOrAddRequested(gid groupsig.ID) (bool, int) {
	if p.requestedMember.Has(gid.GetAddrString()) {
		return true, 0
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	p.requestedMember.Add(gid.GetAddrString())
	return false, p.requestedMember.Size()
}

func newVerifyMsgCache() *verifyMsgCache {
	return &verifyMsgCache{
		verifyMsgs: make([]*model.ConsensusVerifyMessage, 0),
		expire:     time.Now().Add(30 * time.Second),
	}
}

func (c *verifyMsgCache) expired() bool {
	return time.Now().After(c.expire)
}

func (c *verifyMsgCache) addVerifyMsg(msg *model.ConsensusVerifyMessage) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.verifyMsgs = append(c.verifyMsgs, msg)
}

func (c *verifyMsgCache) getVerifyMsgs() []*model.ConsensusVerifyMessage {
	msgs := make([]*model.ConsensusVerifyMessage, len(c.verifyMsgs))
	c.lock.RLock()
	defer c.lock.RUnlock()
	copy(msgs, c.verifyMsgs)
	return msgs
}

func (c *verifyMsgCache) removeVerifyMsgs() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.verifyMsgs = make([]*model.ConsensusVerifyMessage, 0)
}

// castBlockContexts stores the proposal messages for proposal role and the verification context for verify roles
type castBlockContexts struct {
	proposed        *lru.Cache // hash -> *Block, only used for proposal role
	heightVctxs     *lru.Cache // height -> *VerifyContext
	hashVctxs       *lru.Cache // hash -> *VerifyContext
	reservedVctx    *lru.Cache // uint64 -> *VerifyContext, Store the verifyContext that already has the checked out block, to be broadcast
	verifyMsgCaches *lru.Cache // hash -> *verifyMsgCache, Cache verification message
	recentCasted    *lru.Cache // height -> *castedBlock
	chain           types.BlockChain
	tryVerified     *lru.Cache
}

func newCastBlockContexts(chain types.BlockChain) *castBlockContexts {
	return &castBlockContexts{
		proposed:        common.MustNewLRUCache(20),
		heightVctxs:     common.MustNewLRUCacheWithEvictCB(20, heightVctxEvitCallback),
		hashVctxs:       common.MustNewLRUCache(200),
		reservedVctx:    common.MustNewLRUCache(100),
		verifyMsgCaches: common.MustNewLRUCache(200),
		recentCasted:    common.MustNewLRUCache(200),
		tryVerified:     common.MustNewLRUCache(200),
		chain:           chain,
	}
}

func heightVctxEvitCallback(k, v interface{}) {
	ctx := v.(*VerifyContext)
	stdLogger.Debugf("evitVctx: ctx.castHeight=%v, ctx.prevHash=%v, signedMaxQN=%v, signedNum=%v, verifyNum=%v, aggrNum=%v\n", ctx.castHeight, ctx.prevBH.Hash, ctx.getSignedMaxWeight(), ctx.signedNum, ctx.verifyNum, ctx.aggrNum)
}

func (bctx *castBlockContexts) removeReservedVctx(height uint64) {
	bctx.reservedVctx.Remove(height)
}

func (bctx *castBlockContexts) addReservedVctx(vctx *VerifyContext) {
	v, ok := bctx.reservedVctx.Peek(vctx.castHeight)
	if ok {
		stdLogger.Debugf("replace reserved vctx: height=%v, old pre=%v, new pre=%v", vctx.castHeight, v.(*VerifyContext).prevBH.Hash, vctx.prevBH.Hash)
	}
	bctx.reservedVctx.Add(vctx.castHeight, vctx)
}

func (bctx *castBlockContexts) forEachReservedVctx(f func(vctx *VerifyContext) bool) {
	for _, k := range bctx.reservedVctx.Keys() {
		v, ok := bctx.reservedVctx.Peek(k)
		if ok {
			if !f(v.(*VerifyContext)) {
				break
			}
		}
	}
}

func (bctx *castBlockContexts) addProposed(b *types.Block, groupMemberSize int) {
	maxResponseCount := int(math.Ceil(float64(groupMemberSize) / 3)) //only response group member size's 1/3 times
	pb := newProposedBlock(b, maxResponseCount)
	bctx.proposed.Add(b.Header.Hash, pb)
}

func (bctx *castBlockContexts) getProposed(hash common.Hash) *proposedBlock {
	if v, ok := bctx.proposed.Peek(hash); ok {
		return v.(*proposedBlock)
	}
	return nil
}

func (bctx *castBlockContexts) removeProposed(hash common.Hash) {
	bctx.proposed.Remove(hash)
}

func (bctx *castBlockContexts) isHeightCasted(height uint64, pre common.Hash) (cb *castedBlock, casted bool) {
	v, ok := bctx.recentCasted.Peek(height)
	if ok {
		cb := v.(*castedBlock)
		return cb, cb.preHash == pre
	}
	return
}

func (bctx *castBlockContexts) addCastedHeight(height uint64, pre common.Hash) {
	if _, ok := bctx.isHeightCasted(height, pre); !ok {
		bctx.recentCasted.Add(height, &castedBlock{height: height, preHash: pre})
	}
}

func (bctx *castBlockContexts) getVctxByHeight(height uint64) *VerifyContext {
	if v, ok := bctx.heightVctxs.Peek(height); ok {
		return v.(*VerifyContext)
	}
	return nil
}

func (bctx *castBlockContexts) addVctx(vctx *VerifyContext) {
	bctx.heightVctxs.Add(vctx.castHeight, vctx)
}

func (bctx *castBlockContexts) attachVctx(bh *types.BlockHeader, vctx *VerifyContext) {
	bctx.hashVctxs.Add(bh.Hash, vctx)
}

func (bctx *castBlockContexts) getVctxByHash(hash common.Hash) *VerifyContext {
	if v, ok := bctx.hashVctxs.Peek(hash); ok {
		return v.(*VerifyContext)
	}
	return nil
}

func (bctx *castBlockContexts) replaceVerifyCtx(group *verifyGroup, height uint64, expireTime time2.TimeStamp, preBH *types.BlockHeader) *VerifyContext {
	vctx := newVerifyContext(group, height, expireTime, preBH)
	bctx.addVctx(vctx)
	return vctx
}

func (bctx *castBlockContexts) getOrNewVctx(group *verifyGroup, height uint64, expireTime time2.TimeStamp, preBH *types.BlockHeader) *VerifyContext {
	var vctx *VerifyContext
	blog := newBizLog("getOrNewVctx")

	// If the height does not yet have a verifyContext, create one
	if vctx = bctx.getVctxByHeight(height); vctx == nil {
		vctx = newVerifyContext(group, height, expireTime, preBH)
		bctx.addVctx(vctx)
		blog.debug("add vctx expire %v", expireTime)
	} else {
		// In case of hash inconsistency,
		if vctx.prevBH.Hash != preBH.Hash {
			blog.error("vctx pre hash diff, height=%v, existHash=%v, commingHash=%v", height, vctx.prevBH.Hash, preBH.Hash)
			preOld := bctx.chain.QueryBlockHeaderByHash(vctx.prevBH.Hash)
			// The original preBH may be removed by the fork adjustment, then the vctx is invalid, re-use the new preBH
			if preOld == nil {
				blog.debug("replace old vctx because old pre not exist:%v, height=%v", vctx.prevBH.Hash, vctx.prevBH.Height)
				vctx = bctx.replaceVerifyCtx(group, height, expireTime, preBH)
				return vctx
			}
			preNew := bctx.chain.QueryBlockHeaderByHash(preBH.Hash)
			// The new preBH doesn't exist, it may be forked, and it returns nil directly here.
			if preNew == nil {
				blog.debug("discard new block because new pre not exists:%v, height=%v", preBH.Hash, preBH.Height)
				return nil
			}
			// Both old and new preBH are not empty, take high preBH?
			if preOld.Height < preNew.Height {
				blog.debug("replace old vctx because new pre higher than old:new height=%v, old height=%v", preNew.Height, vctx.prevBH.Height)
				vctx = bctx.replaceVerifyCtx(group, height, expireTime, preNew)
			}
		} else {
			if height == 1 && expireTime.After(vctx.expireTime) {
				vctx.expireTime = expireTime
			}
		}
	}
	return vctx
}

func (bctx *castBlockContexts) getOrNewVerifyContext(group *verifyGroup, bh *types.BlockHeader, preBH *types.BlockHeader) *VerifyContext {
	deltaHeightByTime := deltaHeightByTime(bh, preBH)

	expireTime := getCastExpireTime(preBH.CurTime, deltaHeightByTime, bh.Height)

	vctx := bctx.getOrNewVctx(group, bh.Height, expireTime, preBH)
	return vctx
}

func (bctx *castBlockContexts) cleanVerifyContext(height uint64) {
	for _, h := range bctx.heightVctxs.Keys() {
		v, ok := bctx.heightVctxs.Peek(h)
		if !ok {
			continue
		}
		ctx := v.(*VerifyContext)
		bRemove := ctx.shouldRemove(height)
		if bRemove {
			for _, slot := range ctx.GetSlots() {
				bctx.hashVctxs.Remove(slot.BH.Hash)
			}
			ctx.Clear()
			bctx.removeReservedVctx(ctx.castHeight)
			bctx.heightVctxs.Remove(h)
			stdLogger.Debugf("cleanVerifyContext: ctx.castHeight=%v, ctx.prevHash=%v, signedMaxQN=%v, signedNum=%v, verifyNum=%v, aggrNum=%v\n", ctx.castHeight, ctx.prevBH.Hash, ctx.getSignedMaxWeight(), ctx.signedNum, ctx.verifyNum, ctx.aggrNum)
		}
	}
}

func (bctx *castBlockContexts) addVerifyMsg(msg *model.ConsensusVerifyMessage) {
	if v, ok := bctx.verifyMsgCaches.Get(msg.BlockHash); ok {
		c := v.(*verifyMsgCache)
		c.addVerifyMsg(msg)
	} else {
		c := newVerifyMsgCache()
		c.addVerifyMsg(msg)
		bctx.verifyMsgCaches.ContainsOrAdd(msg.BlockHash, c)
	}
}

func (bctx *castBlockContexts) getVerifyMsgCache(hash common.Hash) *verifyMsgCache {
	v, ok := bctx.verifyMsgCaches.Peek(hash)
	if !ok {
		return nil
	}
	return v.(*verifyMsgCache)
}

func (bctx *castBlockContexts) removeVerifyMsgCache(hash common.Hash) {
	bctx.verifyMsgCaches.Remove(hash)
}
