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
	"github.com/darren0718/zvchain/consensus/model"
	"github.com/darren0718/zvchain/log"
)

const ConsensusConfSection = "consensus"

var consensusLogger = log.ConsensusLogger
var stdLogger = log.ConsensusStdLogger
var consensusConfManager common.SectionConfManager

func InitConsensus() {
	consensusLogger = log.ConsensusLogger
	stdLogger = log.ConsensusStdLogger
	cc := common.GlobalConf.GetSectionManager(ConsensusConfSection)
	consensusConfManager = cc
	model.InitParam(cc)
	return
}
