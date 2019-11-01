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

package net

import (
	"fmt"
	"runtime/debug"

	"github.com/darren0718/zvchain/log"
	"github.com/sirupsen/logrus"

	"github.com/darren0718/zvchain/network"
)

var logger *logrus.Logger

// ConsensusHandler used for handling consensus-related messages from network
type ConsensusHandler struct {
	processor MessageProcessor
}

var MessageHandler = new(ConsensusHandler)

func (c *ConsensusHandler) Init(proc MessageProcessor) {
	c.processor = proc
	logger = log.ConsensusStdLogger
}

func (c *ConsensusHandler) Processor() MessageProcessor {
	return c.processor
}

func (c *ConsensusHandler) ready() bool {
	return c.processor != nil && c.processor.Ready()
}

// Handle is the main entrance for handling messages.
// It assigns different types of messages to different processor handlers for processing according to the code field
func (c *ConsensusHandler) Handle(sourceID string, msg network.Message) error {
	code := msg.Code
	body := msg.Body

	var err error
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("error：%v\n", r)
			s := debug.Stack()
			logger.Errorf(string(s))
		}
		if err != nil && logger != nil {
			//logger.Errorf("handle message code %v from %v err: %v", code, sourceID, err)
		}
	}()

	if !c.ready() {
		err = fmt.Errorf("processor not ready yet")
		return err
	}

	switch code {
	case network.CastVerifyMsg:
		m, e := unMarshalConsensusCastMessage(body)
		if e != nil {
			err = e
			return e
		}
		err = c.processor.OnMessageCast(m)
	case network.VerifiedCastMsg:
		m, e := unMarshalConsensusVerifyMessage(body)
		if e != nil {
			err = e
			return e
		}

		err = c.processor.OnMessageVerify(m)
	case network.CastRewardSignReq:
		m, e := unMarshalCastRewardReqMessage(body)
		if e != nil {
			err = e
			return e
		}

		err = c.processor.OnMessageCastRewardSignReq(m)
	case network.CastRewardSignGot:
		m, e := unMarshalCastRewardSignMessage(body)
		if e != nil {
			err = e
			return e
		}

		err = c.processor.OnMessageCastRewardSign(m)
	case network.ReqProposalBlock:
		m, e := unmarshalReqProposalBlockMessage(body)
		if e != nil {
			err = e
			return e
		}
		err = c.processor.OnMessageReqProposalBlock(m, sourceID)

	case network.ResponseProposalBlock:
		m, e := unmarshalResponseProposalBlockMessage(body)
		if e != nil {
			err = e
			return e
		}
		err = c.processor.OnMessageResponseProposalBlock(m)
		logger.Debugf("recv proposal block %v response from %v", m.Hash, sourceID)
	}

	return nil
}
