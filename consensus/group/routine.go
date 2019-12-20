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

// Package group implements the group creation protocol
package group

import (
	"fmt"
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/consensus/base"
	"github.com/darren0718/zvchain/consensus/groupsig"
	"github.com/darren0718/zvchain/consensus/model"
	"github.com/darren0718/zvchain/log"
	"github.com/darren0718/zvchain/middleware/notify"
	"github.com/darren0718/zvchain/middleware/types"
	"github.com/sirupsen/logrus"
	"math"
)

const (
	threshold             = 51  // BLS threshold, percentage number which should divide by 100
	recvPieceMinRatio     = 0.8 // The minimum ratio of the number of participants in the final group-creation to the expected number of nodes
	memberMaxJoinGroupNum = 5   // Maximum number of group one miner can participate in
)

func candidateCount(totalN int) int {
	if totalN >= model.Param.GroupMemberMax {
		return model.Param.GroupMemberMax
	} else if totalN < model.Param.GroupMemberMin {
		return 0
	}
	return totalN
}

func candidateEnough(n int) bool {
	return n >= model.Param.GroupMemberMin
}

func pieceEnough(pieceNum, candidateNum int) bool {
	return pieceNum >= int(math.Ceil(float64(candidateNum)*recvPieceMinRatio))
}

type groupContextProvider interface {
	GetGroupStoreReader() types.GroupStoreReader

	GetGroupPacketSender() types.GroupPacketSender

	RegisterGroupCreateChecker(checker types.GroupCreateChecker)
}

type minerReader interface {
	SelfMinerInfo() *model.SelfMinerDO
	GetLatestVerifyMiner(id groupsig.ID) *model.MinerDO
	GetCanJoinGroupMinersAt(h uint64) []*model.MinerDO
}

type joinedGroupFilter interface {
	MinerJoinedLivedGroupCountFilter(maxCount int, height uint64) func(addr common.Address) bool
}

type createRoutine struct {
	*createChecker
	packetSender types.GroupPacketSender
	groupFilter  joinedGroupFilter
	store        *skStorage
	currID       groupsig.ID
}

var GroupRoutine *createRoutine
var logger *logrus.Logger

func InitRoutine(reader minerReader, chain types.BlockChain, provider groupContextProvider, joinedFilter joinedGroupFilter, miner *model.SelfMinerDO) *skStorage {
	checker := newCreateChecker(reader, chain, provider.GetGroupStoreReader())
	logger = log.GroupLogger
	GroupRoutine = &createRoutine{
		createChecker: checker,
		packetSender:  provider.GetGroupPacketSender(),
		store:         newSkStorage(fmt.Sprintf("groupsk%v.store", common.GlobalConf.GetString("instance", "index", "")), base.Data2CommonHash(miner.SK.Serialize()).Bytes()),
		currID:        miner.ID,
		groupFilter:   joinedFilter,
	}
	top := chain.QueryTopBlock()
	GroupRoutine.updateContext(top)

	go GroupRoutine.store.loop()
	go checker.stat.loop()

	provider.RegisterGroupCreateChecker(checker)

	notify.BUS.Subscribe(notify.BlockAddSucc, GroupRoutine.onBlockAddSuccess)
	notify.BUS.Subscribe(notify.NewTopBlock, GroupRoutine.onNewTopMessage)
	return GroupRoutine.store
}

func (routine *createRoutine) onNewTopMessage(message notify.Message) error {
	bh := message.GetData().(*types.BlockHeader)
	return routine.onNewTopBlock(bh)
}

func (routine *createRoutine) onBlockAddSuccess(message notify.Message) error {
	block := message.GetData().(*types.Block)
	bh := block.Header

	return routine.onNewTopBlock(bh)
}

func (routine *createRoutine) Close() {
	if routine.store != nil {
		routine.store.db.Close()
	}
}

func (routine *createRoutine) onNewTopBlock(bh *types.BlockHeader) error {
	routine.store.blockAddCh <- bh.Height

	routine.updateContext(bh)
	ok, err := routine.checkAndSendEncryptedPiecePacket(bh)
	if err != nil {
		logger.Errorf("check and send encrypted piece error:%v at %v-%v", err, bh.Height, bh.Hash)
	} else {
		if ok {
			logger.Debugf("checkAndSendEncryptedPiecePacket sent encrypted packet at %v, seedHeight %v", bh.Height, routine.currEra().seedHeight)
		}
	}
	ok, err = routine.checkAndSendMpkPacket(bh)
	if err != nil {
		logger.Errorf("check and send mpk error:%v at %v-%v", err, bh.Height, bh.Hash)
	} else {
		if ok {
			logger.Debugf("checkAndSendMpkPacket sent mpk packet at %v, seedHeight %v", bh.Height, routine.currEra().seedHeight)
		}
	}
	ok, err = routine.checkAndSendOriginPiecePacket(bh)
	if err != nil {
		logger.Errorf("check and send origin piece error:%v at %v-%v", err, bh.Height, bh.Hash)
	} else {
		if ok {
			logger.Debugf("checkAndSendOriginPiecePacket sent origin packet at %v, seedHeight %v", bh.Height, routine.currEra().seedHeight)
		}
	}
	return err
}

// UpdateEra updates the era info base on current block header
func (routine *createRoutine) updateContext(bh *types.BlockHeader) {
	routine.lock.Lock()
	defer routine.lock.Unlock()

	sh := seedHeight(bh.Height)
	seedBH := routine.chain.QueryBlockHeaderByHeight(sh)
	if routine.ctx != nil {
		curEra := routine.currEra()
		if curEra.sameEra(sh, seedBH) {
			return
		}
	}

	seedBlockHash := common.Hash{}
	if seedBH != nil {
		seedBlockHash = seedBH.Hash
	}
	routine.ctx = newCreateContext(newEra(sh, seedBH))
	era := routine.currEra()
	routine.stat.markStatus(era.seedHeight, createStatusIdle)

	logger.Debugf("new create context: era:%v %v-%v %v %v %v %v", bh.Height, sh, seedBlockHash, era.encPieceRange, era.mpkRange, era.oriPieceRange, era.endRange)
	err := routine.selectCandidates()
	if err != nil {
		logger.Debugf("select candidates:%v", err)
	}
	routine.stat.outCh <- struct{}{}
}

func (routine *createRoutine) selectCandidates() error {
	routine.ctx.cands = make(candidates, 0)

	era := routine.currEra()
	if !era.seedExist() {
		return fmt.Errorf("seed block not exist:%v", era.seedHeight)
	}

	h := era.seedHeight
	bh := era.seedBlock

	allVerifiers := routine.minerReader.GetCanJoinGroupMinersAt(h)
	if !candidateEnough(len(allVerifiers)) {
		return fmt.Errorf("not enought candidate in all:%v", len(allVerifiers))
	}

	availCandidates := make([]*model.MinerDO, 0)
	filterFun := routine.groupFilter.MinerJoinedLivedGroupCountFilter(memberMaxJoinGroupNum, h)

	for _, m := range allVerifiers {
		if filterFun(m.ID.ToAddress()) {
			availCandidates = append(availCandidates, m)
		}
	}

	memberCnt := candidateCount(len(availCandidates))
	if !candidateEnough(memberCnt) {
		return fmt.Errorf("not enough candiates in availables:%v", len(availCandidates))
	}

	selector := newCandidateSelector(availCandidates, bh.Random)
	selectedCandidates := selector.fts(memberCnt)

	mems := make([]string, len(selectedCandidates))
	for _, m := range selectedCandidates {
		mems = append(mems, m.ID.GetAddrString())
	}

	routine.ctx.cands = selectedCandidates
	routine.ctx.selected = routine.ctx.cands.has(routine.currID)
	logger.Debugf("selected candidates size %v, at seed %v-%v is %v", routine.ctx.cands.size(), era.seedHeight, era.Seed(), mems)
	return nil
}

func (routine *createRoutine) selected() bool {
	return routine.ctx.selected
}

func (routine *createRoutine) checkAndSendEncryptedPiecePacket(bh *types.BlockHeader) (bool, error) {
	routine.lock.Lock()
	defer routine.lock.Unlock()

	era := routine.currEra()
	if !era.seedExist() {
		return false, fmt.Errorf("seed not exists:%v", era.seedHeight)
	}
	if !era.encPieceRange.inRange(bh.Height) {
		return false, nil
	}

	if !routine.shouldCreateGroup() {
		return false, nil
	}
	// Was selected
	if !routine.selected() {
		return false, nil
	}

	mInfo := routine.minerReader.SelfMinerInfo()
	if mInfo == nil {
		return false, fmt.Errorf("miner is nil")
	}
	if !mInfo.CanJoinGroup() {
		logger.Debugf("miner info:%+v", mInfo.MinerDO)
		return false, fmt.Errorf("current miner cann't join group")
	}

	// Has sent piece
	if routine.ctx.sentEncryptedPiecePacket != nil || routine.storeReader.HasSentEncryptedPiecePacket(mInfo.ID.Serialize(), era) {
		return false, nil
	}

	encSk := generateEncryptedSeckey()

	// Generate encrypted share piece
	packet := generateEncryptedSharePiecePacket(mInfo, encSk, era.Seed(), routine.ctx.cands)
	routine.store.storeSeckey(era.Seed(), nil, &encSk, types.DismissEpochOfGroupsCreatedAt(bh.Height).Start())

	// Send the piece packet
	err := routine.packetSender.SendEncryptedPiecePacket(packet)
	if err != nil {
		return false, fmt.Errorf("send packet error:%v", err)
	}
	routine.ctx.sentEncryptedPiecePacket = packet

	pkt := packet.(*encryptedSharePiecePacket)
	logger.Debugf("send encrypted share piece: %v %v %v %v", pkt.sender, pkt.seed, pkt.pubkey0.GetHexString(), common.ToHex(pkt.Pieces()))
	for i, p := range pkt.pieces {
		logger.Debugf("receiver %v %v is %v", i, routine.ctx.cands[i].ID, p.GetHexString())

	}

	return true, nil
}

func (routine *createRoutine) checkAndSendMpkPacket(bh *types.BlockHeader) (bool, error) {
	routine.lock.Lock()
	defer routine.lock.Unlock()

	era := routine.currEra()
	if !era.seedExist() {
		return false, fmt.Errorf("seed not exists:%v", era.seedHeight)
	}
	if !era.mpkRange.inRange(bh.Height) {
		return false, nil
	}
	if !routine.shouldCreateGroup() {
		return false, nil
	}
	cands := routine.ctx.cands

	// Was selected
	if !routine.selected() {
		return false, nil
	}

	mInfo := routine.minerReader.SelfMinerInfo()
	if mInfo == nil {
		return false, fmt.Errorf("miner is nil")
	}
	if !mInfo.CanJoinGroup() {
		return false, fmt.Errorf("current miner cann't join group")
	}

	// Has sent mpk
	if routine.ctx.sentMpkPacket != nil || routine.storeReader.HasSentMpkPacket(mInfo.ID.Serialize(), era) {
		return false, nil
	}
	// Didn't sent share piece
	if routine.ctx.sentEncryptedPiecePacket == nil && !routine.storeReader.HasSentEncryptedPiecePacket(mInfo.ID.Serialize(), era) {
		return false, fmt.Errorf("didn't send encrypted piece")
	}

	encryptedPackets, err := routine.storeReader.GetEncryptedPiecePackets(era)
	if err != nil {
		return false, fmt.Errorf("get receiver piece error:%v", err)
	}

	num := len(encryptedPackets)
	logger.Debugf("get encrypted pieces size %v at %v-%v", num, era.seedHeight, era.Seed())

	// Check if the received pieces enough
	if !pieceEnough(num, cands.size()) {
		return false, fmt.Errorf("received piece not enough, recv %v, total %v", num, cands.size())
	}

	msk, err := aggrSignSecKeyWithMySK(encryptedPackets, cands.find(mInfo.ID), mInfo.SK)
	if err != nil {
		return false, fmt.Errorf("genearte msk error:%v", err)
	}
	routine.store.storeSeckey(era.Seed(), msk, nil, types.DismissEpochOfGroupsCreatedAt(bh.Height).Start())

	mpk := groupsig.NewPubkeyFromSeckey(*msk)
	// Generate encrypted share piece
	packet := &mpkPacket{
		sender: mInfo.ID,
		seed:   era.Seed(),
		mPk:    *mpk,
		sign:   groupsig.Sign(*msk, era.Seed().Bytes()),
	}

	// Send the piece packet
	err = routine.packetSender.SendMpkPacket(packet)
	if err != nil {
		return false, fmt.Errorf("send mpk packet error:%v", err)
	}
	routine.ctx.sentMpkPacket = packet

	logger.Debugf("send mpk: %v %v %v %v msk: %v", packet.sender, packet.seed, packet.mPk.GetHexString(), msk.GetHexString(), packet.sign.GetHexString())

	return true, nil
}

func (routine *createRoutine) checkAndSendOriginPiecePacket(bh *types.BlockHeader) (bool, error) {
	routine.lock.Lock()
	defer routine.lock.Unlock()

	era := routine.currEra()
	if !era.seedExist() {
		return false, fmt.Errorf("seed not exists:%v", era.seedHeight)
	}
	if !era.oriPieceRange.inRange(bh.Height) {
		return false, nil
	}
	if !routine.shouldCreateGroup() {
		return false, nil
	}
	// Was selected
	if !routine.selected() {
		return false, nil
	}
	mInfo := routine.minerReader.SelfMinerInfo()
	if mInfo == nil {
		return false, fmt.Errorf("miner is nil")
	}
	if !mInfo.CanJoinGroup() {
		return false, fmt.Errorf("current miner cann't join group")
	}

	// Whether origin piece required
	if !routine.storeReader.IsOriginPieceRequired(era) {
		return false, nil
	}
	id := mInfo.ID.Serialize()
	// Whether sent encrypted pieces
	if !routine.storeReader.HasSentEncryptedPiecePacket(id, era) {
		return false, fmt.Errorf("didn't sent encrypted share piece")
	}
	// Whether sent mpk packet
	if !routine.storeReader.HasSentMpkPacket(id, era) {
		return false, fmt.Errorf("didn't sent mpk packet")
	}
	// Has sent piece
	if routine.ctx.sentOriginPiecePacket != nil || routine.storeReader.HasSentOriginPiecePacket(id, era) {
		return false, nil
	}

	ski := routine.store.getSkInfo(era.Seed())
	if ski == nil {
		return false, fmt.Errorf("has no encrypted seckey")
	}

	// Generate origin share piece
	sp := generateSharePiecePacket(mInfo, ski.encSk, era.Seed(), routine.ctx.cands)
	packet := &originSharePiecePacket{sharePiecePacket: sp}

	// Send the piece packet
	err := routine.packetSender.SendOriginPiecePacket(packet)
	if err != nil {
		return false, fmt.Errorf("send packet error:%v", err)
	}
	routine.ctx.sentOriginPiecePacket = packet

	return true, nil
}

func GetCandidates() candidates {
	candidates := make(candidates, 0)

	if GroupRoutine != nil {
		for _, v := range GroupRoutine.ctx.cands {
			candidates = append(candidates, v)
		}
		return candidates
	}
	return nil
}
