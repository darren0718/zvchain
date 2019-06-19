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

package cli

import (
	"fmt"

	"github.com/zvchain/zvchain/common"
	"github.com/zvchain/zvchain/consensus/groupsig"
	"github.com/zvchain/zvchain/consensus/mediator"
	"github.com/zvchain/zvchain/core"
	"github.com/zvchain/zvchain/middleware/types"
)

func convertTransaction(tx *types.Transaction) *Transaction {
	trans := &Transaction{
		Hash:          tx.Hash,
		Source:        tx.Source,
		Target:        tx.Target,
		Type:          tx.Type,
		GasLimit:      tx.GasLimit.Uint64(),
		GasPrice:      tx.GasPrice.Uint64(),
		Data:          tx.Data,
		ExtraData:     tx.ExtraData,
		ExtraDataType: tx.ExtraDataType,
		Nonce:         tx.Nonce,
		Value:         common.RA2TAS(tx.Value.Uint64()),
	}
	return trans
}

func convertBlockHeader(b *types.Block) *Block {
	bh := b.Header
	block := &Block{
		Height:  bh.Height,
		Hash:    bh.Hash,
		PreHash: bh.PreHash,
		CurTime: bh.CurTime.Local(),
		PreTime: bh.PreTime().Local(),
		Castor:  groupsig.DeserializeID(bh.Castor),
		GroupID: groupsig.DeserializeID(bh.GroupID),
		Prove:   common.ToHex(bh.ProveValue),
		TotalQN: bh.TotalQN,
		TxNum:   uint64(len(b.Transactions)),
		//Qn: mediator.Proc.CalcBlockHeaderQN(bh),
		StateRoot:   bh.StateTree,
		TxRoot:      bh.TxTree,
		ReceiptRoot: bh.ReceiptTree,
		Random:      common.ToHex(bh.Random),
	}
	return block
}

func convertBonusTransaction(tx *types.Transaction) *BonusTransaction {
	if tx.Type != types.TransactionTypeBonus {
		return nil
	}
	gid, ids, bhash, value, err := mediator.Proc.MainChain.GetBonusManager().ParseBonusTransaction(tx)
	if err != nil {
		return nil
	}
	targets := make([]groupsig.ID, len(ids))
	for i, id := range ids {
		targets[i] = groupsig.DeserializeID(id)
	}
	return &BonusTransaction{
		Hash:      tx.Hash,
		BlockHash: bhash,
		GroupID:   groupsig.DeserializeID(gid),
		TargetIDs: targets,
		Value:     value.Uint64(),
	}
}

func genMinerBalance(id groupsig.ID, bh *types.BlockHeader) *MinerBonusBalance {
	mb := &MinerBonusBalance{
		ID: id,
	}
	db, err := mediator.Proc.MainChain.GetAccountDBByHash(bh.Hash)
	if err != nil {
		common.DefaultLogger.Errorf("GetAccountDBByHash err %v, hash %v", err, bh.Hash)
		return mb
	}
	mb.CurrBalance = db.GetBalance(id.ToAddress())
	preDB, err := mediator.Proc.MainChain.GetAccountDBByHash(bh.PreHash)
	if err != nil {
		common.DefaultLogger.Errorf("GetAccountDBByHash err %v hash %v", err, bh.PreHash)
		return mb
	}
	mb.PreBalance = preDB.GetBalance(id.ToAddress())
	return mb
}

func sendTransaction(trans *types.Transaction) error {
	if trans.Sign == nil {
		return fmt.Errorf("transaction sign is empty")
	}

	if ok, err := core.BlockChainImpl.GetTransactionPool().AddTransaction(trans); err != nil || !ok {
		common.DefaultLogger.Errorf("AddTransaction not ok or error:%s", err.Error())
		return err
	}
	return nil
}