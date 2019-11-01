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

	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/middleware/notify"
	"github.com/darren0718/zvchain/middleware/types"
)

func (chain *FullBlockChain) initMessageHandler() {
	notify.BUS.Subscribe(notify.BlockAddSucc, chain.onBlockAddSuccess)
	notify.BUS.Subscribe(notify.NewBlock, chain.newBlockHandler)
}

func (chain *FullBlockChain) newBlockHandler(msg notify.Message) error {
	m := notify.AsDefault(msg)

	source := m.Source()
	key := string(common.Sha256(m.Body()))
	exist, _ := chain.newBlockMessages.ContainsOrAdd(key, 1)
	if exist {
		Logger.Debugf("Rcv new duplicate block from %s, key:%v", source, key)
		return nil
	}

	block, e := types.UnMarshalBlock(m.Body())
	if e != nil {
		err := fmt.Errorf("UnMarshal block error:%s", e.Error())
		Logger.Error(err)
		return err
	}

	Logger.Debugf("Rcv new block from %s,hash:%v,height:%d,totalQn:%d,tx len:%d", source, block.Header.Hash.Hex(), block.Header.Height, block.Header.TotalQN, len(block.Transactions))
	chain.AddBlockOnChain(source, block)
	return nil
}
