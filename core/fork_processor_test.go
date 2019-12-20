//   Copyright (C) 2019 ZVChain
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
	"github.com/darren0718/zvchain/consensus/base"
	"github.com/darren0718/zvchain/consensus/groupsig"
	"github.com/darren0718/zvchain/log"
	"github.com/darren0718/zvchain/middleware"
	"github.com/darren0718/zvchain/middleware/notify"
	"github.com/darren0718/zvchain/middleware/types"
	"github.com/darren0718/zvchain/network"
	"github.com/sirupsen/logrus"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

var groupReader activatedGroupReader
var wg *sync.WaitGroup

func init() {
	Logger = logrus.StandardLogger()
	middleware.InitMiddleware()
	groupReader = initGroupReader4CPTest(400)
	initPeerManager()
	wg = &sync.WaitGroup{}
	log.ELKLogger.SetLevel(logrus.ErrorLevel)
}

var (
	chains     = make(map[string]*FullBlockChain)
	chainPath1 = "d_b"
	chainPath2 = "d_b2"
	smallPath1 = "sm_d_b"
	smallPath2 = "sm_d_b2"
	id1        = "1"
	id2        = "2"
	db_cache1  = "db_cache1"
	db_cache2  = "db_cache2"
)

type msgSender4Test struct {
	myId string
}

func (s *msgSender4Test) Send(id string, msg network.Message) error {
	fp := chains[id].forkProcessor
	notifyMsg := notify.NewDefaultMessage(msg.Body, s.myId, 1, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		switch msg.Code {
		case network.ForkFindAncestorResponse:
			fp.onFindAncestorResponse(notifyMsg)
		case network.ForkFindAncestorReq:
			fp.onFindAncestorReq(notifyMsg)
		case network.ForkChainSliceReq:
			fp.onChainSliceRequest(notifyMsg)
		case network.ForkChainSliceResponse:
			fp.onChainSliceResponse(notifyMsg)
		}
	}()
	return nil
}

func initChain(dataPath string, smallPath, db_cache, id string) *FullBlockChain {
	common.InitConf("test1.ini")
	common.GlobalConf.SetBool(configSec, "prune_mode", false)
	common.GlobalConf.SetString(configSec, "db_blocks", dataPath)
	common.GlobalConf.SetString(configSec, "db_cache", db_cache)
	common.GlobalConf.SetString(configSec, "small_db", smallPath)
	common.GlobalConf.SetInt(configSec, "db_node_cache", 0)
	err := initBlockChain(NewConsensusHelper4Test(groupsig.ID{}), nil)
	clearTicker()
	Logger = logrus.StandardLogger()
	if err != nil {
		Logger.Panicf("init chain error:%v", err)
	}
	chain := BlockChainImpl
	notify.BUS = notify.NewBus()
	Logger = logrus.StandardLogger()
	// mock the tvm stateProc
	tvm := newStateProcessor(chain)
	// mock the cp checker
	chain.cpChecker = newCpChecker(groupReader, chain)

	tvm.addPostProcessor(chain.cpChecker.updateVotes)
	chain.stateProc = tvm

	// mock fork process
	fh := &forkProcessor{
		chain:     chain,
		verifier:  chain.consensusHelper,
		peerCP:    chain.cpChecker,
		msgSender: &msgSender4Test{myId: id},
		logger:    logrus.StandardLogger(),
	}
	chain.forkProcessor = fh

	chain.cpChecker.init()
	chains[id] = chain

	return chain
}

func addRandomBlock(chain *FullBlockChain, h uint64) {
	pv := make([]byte, base.VRFProveSize)
	rand.Read(pv)
	castor := common.Address{}
	groups := chain.cpChecker.groupReader.GetActivatedGroupsAt(h)
	Logger.Debugf("group size is %v at %v, epoch start %v-%v", len(groups), h, types.EpochAt(h).Start(), types.EpochAt(h).End())
	selectGroupIndex := rand.Int31n(int32(len(groups)))
	b := chain.CastBlock(h, pv, uint64(rand.Int31n(6)), castor.Bytes(), groups[selectGroupIndex].Header().Seed())
	if b == nil {
		return
	}
	ret := chain.AddBlockOnChain("", b)
	if ret != types.AddBlockSucc {
		Logger.Panicf("add block fail: %v %v", h, ret)
	}
}

func buildChain(height uint64, chain *FullBlockChain) {
	if chain.Height() > height {
		return
	}
	for h := chain.Height() + 1; h < height; h++ {
		addRandomBlock(chain, h)
	}
}

func forkChain(heightLimit uint64, forkLength uint64, chain *FullBlockChain) {
	top := chain.Height()
	ancestor := chain.QueryBlockHeaderFloor(top - forkLength)
	chain.ResetTop(ancestor)

	buildChain(heightLimit, chain)
}
func TestPathFork(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	fmt.Println("Current test filename: " + filename)
}

func TestBuildChain(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	t.Log(chain.Height(), chain.QueryTopBlock().Hash)

	buildChain(400, chain)
	t.Log(chain.Height(), chain.QueryTopBlock().Hash)
}

func TestScanBlocks(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	for h := uint64(3990); h <= chain.Height(); h++ {
		b := chain.QueryBlockHeaderByHeight(h)
		if b == nil {
			continue
		}
		t.Log(b.Height, b.Hash, b.Group, b.TotalQN)
	}
	clearSelf(t)
	t.Log("============================================")
	chain = initChain(chainPath2, smallPath2, db_cache2, id2)
	for h := uint64(3990); h <= chain.Height(); h++ {
		b := chain.QueryBlockHeaderByHeight(h)
		if b == nil {
			continue
		}
		t.Log(b.Height, b.Hash, b.Group, b.TotalQN)
	}
}

func TestForkChain(t *testing.T) {
	chain := initChain(chainPath2, smallPath2, db_cache2, id2)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	t.Log(chain.Height(), chain.QueryTopBlock().Hash)

	forkChain(chain.Height(), 3, chain)
}

func build2Chains(chain1Limit, chain2Limit uint64, forkLength uint64) (chain1, chain2 *FullBlockChain) {
	chain1 = initChain(chainPath1, smallPath1, db_cache1, id1)
	buildChain(chain1Limit, chain1)
	Logger.Infof("chain1 top:%v %v", chain1.QueryTopBlock().Height, chain1.QueryTopBlock().Hash)

	os.RemoveAll(chainPath2)
	os.RemoveAll("small_db")
	err := exec.Command("cp", "-rf", chainPath1, chainPath2).Run()
	if err != nil {
		Logger.Error(err)
	}

	chain2 = initChain(chainPath2, smallPath2, db_cache2, id2)
	forkChain(chain2Limit, forkLength, chain2)
	Logger.Infof("chain2 top:%v %v", chain2.QueryTopBlock().Height, chain2.QueryTopBlock().Hash)
	return
}

func clearDatas() {
	os.RemoveAll(chainPath1)
	os.RemoveAll(db_cache1)
	os.RemoveAll(db_cache2)
	os.RemoveAll(smallPath1)
	os.RemoveAll(smallPath2)
	os.RemoveAll(chainPath2)
	os.RemoveAll("logs")
}

func TestForkProcess_OnFindAncestorReq_GoodMessage(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	buildChain(1000, chain)

	fp := chain.forkProcessor

	pieces := fp.getLocalPieceInfo(chain.QueryTopBlock().Hash)
	pieceReq := &findAncestorPieceReq{
		ChainPiece: pieces,
		ReqCnt:     int32(10),
	}

	body, e := marshalFindAncestorReqInfo(pieceReq)
	if e != nil {
		fp.logger.Errorf("Marshal chain piece info error:%s!", e.Error())
		fp.reset()
		return
	}

	message := network.Message{Code: network.ForkFindAncestorReq, Body: body}

	msg := notify.NewDefaultMessage(message.Body, id1, 1, 1)
	err := chain.forkProcessor.onFindAncestorReq(msg)
	if err != nil {
		t.Errorf("process error %v", err)
	}
}

func TestForkProcess_OnFindAncestorReq_BadMessage(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	buildChain(1000, chain)

	randBytes := make([]byte, rand.Int31n(100))
	rand.Read(randBytes)
	message := network.Message{Code: network.ForkFindAncestorReq, Body: randBytes}

	msg := notify.NewDefaultMessage(message.Body, id1, 1, 1)
	err := chain.forkProcessor.onFindAncestorReq(msg)
	if err == nil {
		t.Errorf("should be error with random input")
	}
}

func TestForkProcess_OnFindAncestorResponse_Found_GoodMessage(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	ctx := &forkSyncContext{
		target:       id2,
		lastReqPiece: &findAncestorPieceReq{},
		targetTop:    newTopBlockInfo(chain.QueryTopBlock()),
		localCP:      chain.CheckPointAt(chain.Height()),
	}
	fp := chain.forkProcessor
	fp.syncCtx = ctx

	resp := &findAncestorBlockResponse{
		TopHeader:    chain.QueryTopBlock(),
		FindAncestor: true,
		Blocks:       []*types.Block{{Header: chain.QueryTopBlock()}},
	}
	bs, err := marshalFindAncestorBlockResponseMsg(resp)
	if err != nil {
		t.Errorf("marshal error %v", err)
	}

	msg := notify.NewDefaultMessage(bs, id2, 1, 1)

	err = chain.forkProcessor.onFindAncestorResponse(msg)
	if err != nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnFindAncestorResponse_NotFound_GoodMessage(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)

	ctx := &forkSyncContext{
		target:       id2,
		lastReqPiece: &findAncestorPieceReq{ChainPiece: []common.Hash{chain.QueryTopBlock().Hash}},
		targetTop:    newTopBlockInfo(chain.QueryTopBlock()),
		localCP:      chain.CheckPointAt(chain.Height()),
	}
	fp := chain.forkProcessor
	fp.syncCtx = ctx

	resp := &findAncestorBlockResponse{
		TopHeader:    chain.QueryTopBlock(),
		FindAncestor: false,
		Blocks:       []*types.Block{},
	}
	bs, err := marshalFindAncestorBlockResponseMsg(resp)
	if err != nil {
		t.Errorf("marshal error %v", err)
	}

	msg := notify.NewDefaultMessage(bs, id2, 1, 1)

	err = chain.forkProcessor.onFindAncestorResponse(msg)
	if err != nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnChainSliceReq_GoodMessage(t *testing.T) {
	clearDatas()
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	fp := chain.forkProcessor

	req := &chainSliceReq{
		begin: 3990,
		end:   4000,
	}
	bs, err := marshalChainSliceReqMsg(req)
	if err != nil {
		t.Errorf("marshal error %v", err)
	}

	msg := notify.NewDefaultMessage(bs, id2, 1, 1)

	err = fp.onChainSliceRequest(msg)
	if err != nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnChainSliceReq_BadMessage_Range(t *testing.T) {
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	fp := chain.forkProcessor

	req := &chainSliceReq{
		begin: 1000,
		end:   10000000,
	}
	bs, err := marshalChainSliceReqMsg(req)
	if err != nil {
		t.Errorf("marshal error %v", err)
	}

	msg := notify.NewDefaultMessage(bs, id2, 1, 1)

	err = fp.onChainSliceRequest(msg)
	if err != nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnChainSliceReq_BadMessage_Random(t *testing.T) {
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	fp := chain.forkProcessor

	randBytes := make([]byte, rand.Int31n(1000))
	rand.Read(randBytes)
	msg := notify.NewDefaultMessage(randBytes, id2, 1, 1)

	err := fp.onChainSliceRequest(msg)
	if err == nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnChainSliceResponse_GoodMessage(t *testing.T) {
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	buildChain(400, chain)
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)

	fp := chain.forkProcessor

	ctx := &forkSyncContext{
		target:       id2,
		lastReqPiece: &findAncestorPieceReq{ChainPiece: []common.Hash{chain.QueryTopBlock().Hash}},
		targetTop:    newTopBlockInfo(chain.QueryTopBlock()),
		localCP:      chain.CheckPointAt(chain.Height()),
		ancestor:     chain.QueryTopBlock(),
	}
	fp.syncCtx = ctx

	resp := &blockResponseMessage{
		Blocks: make([]*types.Block, 0),
	}
	for h := uint64(190); h < 200; h++ {
		b := chain.QueryBlockByHeight(h)
		if b == nil {
			continue
		}
		resp.Blocks = append(resp.Blocks, b)
	}
	bs, err := marshalBlockMsgResponse(resp)
	if err != nil {
		t.Errorf("marshal error %v", err)
	}

	msg := notify.NewDefaultMessage(bs, id2, 1, 1)

	err = fp.onChainSliceResponse(msg)
	if err != nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnChainEmptySliceResponse(t *testing.T) {
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	fp := chain.forkProcessor

	ctx := &forkSyncContext{
		target:       id2,
		lastReqPiece: &findAncestorPieceReq{ChainPiece: []common.Hash{chain.QueryTopBlock().Hash}},
		targetTop:    newTopBlockInfo(chain.QueryTopBlock()),
		localCP:      chain.CheckPointAt(chain.Height()),
		ancestor:     chain.QueryTopBlock(),
	}
	fp.syncCtx = ctx

	resp := &blockResponseMessage{}

	bs, err := marshalBlockMsgResponse(resp)
	if err != nil {
		t.Errorf("marshal error %v", err)
	}
	randBytes := make([]byte, rand.Int31n(1000))
	rand.Read(randBytes)

	msg := notify.NewDefaultMessage(bs, id2, 1, 1)

	err = fp.onChainSliceResponse(msg)
	if err != nil {
		t.Errorf("handle error %v", err)
	}
}

func TestForkProcess_OnChainSliceResponse_BadMessage(t *testing.T) {
	chain := initChain(chainPath1, smallPath1, db_cache1, id1)
	_ = initChain(chainPath2, smallPath2, db_cache2, id2)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	fp := chain.forkProcessor
	ctx := &forkSyncContext{
		target:       id2,
		lastReqPiece: &findAncestorPieceReq{ChainPiece: []common.Hash{chain.QueryTopBlock().Hash}},
		targetTop:    newTopBlockInfo(chain.QueryTopBlock()),
		localCP:      chain.CheckPointAt(chain.Height()),
		ancestor:     chain.QueryTopBlock(),
	}
	fp.syncCtx = ctx
	randBytes := make([]byte, rand.Int31n(1000))
	rand.Read(randBytes)
	msg := notify.NewDefaultMessage(randBytes, id2, 1, 1)

	err := fp.onChainSliceResponse(msg)
	t.Log(err)
	if err == nil {
		t.Errorf("should be error with bad message")
	}
}

func TestForkProcess_TryProcess_LocalMoreWeight(t *testing.T) {
	chain1, chain2 := build2Chains(2000, 1990, 15)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	fp1 := chain1.forkProcessor
	ret := fp1.tryToProcessFork(id2, &types.Block{Header: chain2.QueryTopBlock()})
	if ret {
		t.Errorf("should not process fork")
	}
}

func TestForkProcess_TryProcess_LocalCPHigher(t *testing.T) {
	chain1, chain2 := build2Chains(3000, 3010, 16)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	top1 := chain1.QueryTopBlock()
	top2 := chain2.QueryTopBlock()
	Logger.Infof("before fork process chain1 top %v %v", top1.Hash, top1.Height)
	Logger.Infof("before fork process chain2 top %v %v", top2.Hash, top2.Height)
	fp1 := chain1.forkProcessor
	ret := fp1.tryToProcessFork(id2, &types.Block{Header: chain2.QueryTopBlock()})
	if !ret {
		t.Errorf("should process fork")
	}
	wg.Wait()

	afterForkTop1 := chain1.QueryTopBlock()
	Logger.Infof("after fork process chain1 top %v %v", afterForkTop1.Hash, afterForkTop1.Height)
	if afterForkTop1.Hash != top1.Hash {
		t.Errorf("chain top change after fork process")
	}
	time.Sleep(2 * time.Second)
}

func TestForkProcess_TryProcess_ShortFork_Accepted(t *testing.T) {
	chain1, chain2 := build2Chains(3000, 3010, 4)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	top1 := chain1.QueryTopBlock()
	top2 := chain2.QueryTopBlock()
	Logger.Infof("before fork process chain1 top %v %v", top1.Hash, top1.Height)
	Logger.Infof("before fork process chain2 top %v %v", top2.Hash, top2.Height)
	fp1 := chain1.forkProcessor
	ret := fp1.tryToProcessFork(id2, &types.Block{Header: chain2.QueryTopBlock()})
	if !ret {
		t.Errorf("should process fork")
	}
	wg.Wait()

	afterForkTop1 := chain1.QueryTopBlock()
	Logger.Infof("after fork process chain1 top %v %v", afterForkTop1.Hash, afterForkTop1.Height)
	if afterForkTop1.Hash != top2.Hash {
		t.Errorf("fork process fail, should accept peer fork")
	}
	time.Sleep(2 * time.Second)
}

func TestForkProcess_TryProcess_ShortFork_MultiRequestChainSlice_Accepted(t *testing.T) {
	chain1, chain2 := build2Chains(3000, 3060, 6)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	top1 := chain1.QueryTopBlock()
	top2 := chain2.QueryTopBlock()
	Logger.Infof("before fork process chain1 top %v %v", top1.Hash, top1.Height)
	Logger.Infof("before fork process chain2 top %v %v", top2.Hash, top2.Height)
	fp1 := chain1.forkProcessor
	ret := fp1.tryToProcessFork(id2, &types.Block{Header: chain2.QueryTopBlock()})
	if !ret {
		t.Errorf("should process fork")
	}
	wg.Wait()

	afterForkTop1 := chain1.QueryTopBlock()
	Logger.Infof("after fork process chain1 top %v %v", afterForkTop1.Hash, afterForkTop1.Height)
	if afterForkTop1.Hash != top2.Hash {
		t.Errorf("fork process fail, should accept peer fork")
	}
	time.Sleep(2 * time.Second)
}

func TestForkProcess_TryProcess_PeerLongFork_Accepted(t *testing.T) {
	chain1, chain2 := build2Chains(3000, 4000, 6)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	top1 := chain1.QueryTopBlock()
	top2 := chain2.QueryTopBlock()
	Logger.Infof("before fork process chain1 top %v %v", top1.Hash, top1.Height)
	Logger.Infof("before fork process chain2 top %v %v", top2.Hash, top2.Height)
	fp1 := chain1.forkProcessor
	ret := fp1.tryToProcessFork(id2, &types.Block{Header: chain2.QueryTopBlock()})
	if !ret {
		t.Errorf("should process fork")
	}
	wg.Wait()

	afterForkTop1 := chain1.QueryTopBlock()
	Logger.Infof("after fork process chain1 top %v %v", afterForkTop1.Hash, afterForkTop1.Height)
	if !chain2.HasBlock(afterForkTop1.Hash) {
		t.Errorf("fork process fail, should accept peer fork")
	}
	time.Sleep(2 * time.Second)
}

func TestForkProcess_TryProcess_UnAcceptable(t *testing.T) {
	chain1, chain2 := build2Chains(3000, 4000, 500)
	defer func() {
		clearSelf(t)
		clearDatas()
	}()
	top1 := chain1.QueryTopBlock()
	top2 := chain2.QueryTopBlock()
	Logger.Infof("before fork process chain1 top %v %v", top1.Hash, top1.Height)
	Logger.Infof("before fork process chain2 top %v %v", top2.Hash, top2.Height)
	fp1 := chain1.forkProcessor
	ret := fp1.tryToProcessFork(id2, &types.Block{Header: chain2.QueryTopBlock()})
	if !ret {
		t.Errorf("should process fork")
	}
	wg.Wait()

	afterForkTop1 := chain1.QueryTopBlock()
	Logger.Infof("after fork process chain1 top %v %v", afterForkTop1.Hash, afterForkTop1.Height)
	if chain2.HasBlock(afterForkTop1.Hash) {
		t.Errorf("shouldn't accept fork for cp reason")
	}
}
