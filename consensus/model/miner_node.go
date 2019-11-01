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

package model

import (
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/consensus/base"
	"github.com/darren0718/zvchain/consensus/groupsig"
	"github.com/darren0718/zvchain/middleware/types"
)

// MinerDO defines the important infos for one miner
type MinerDO struct {
	PK          groupsig.Pubkey
	VrfPK       base.VRFPublicKey
	ID          groupsig.ID
	Stake       uint64
	NType       types.MinerType
	ApplyHeight uint64
	Status      types.MinerStatus
}

func (md *MinerDO) IsActive() bool {
	return md.Status == types.MinerStatusActive
}

// CanPropose means whether it can be cast block at this height
func (md *MinerDO) CanPropose() bool {
	return md.IsProposal() && md.IsActive()
}

// CanJoinGroup means whether it can join the group at this height
func (md *MinerDO) CanJoinGroup() bool {
	return md.IsVerifier() && md.IsActive()
}

func (md *MinerDO) IsVerifier() bool {
	return md.NType == types.MinerTypeVerify
}

func (md *MinerDO) IsProposal() bool {
	return md.NType == types.MinerTypeProposal
}

// SelfMinerDO inherited from MinerDO.
// And some private key included
type SelfMinerDO struct {
	MinerDO
	SecretSeed base.Rand // Private random number
	SK         groupsig.Seckey
	VrfSK      base.VRFPrivateKey
}

func (mi *SelfMinerDO) Read(p []byte) (n int, err error) {
	bs := mi.SecretSeed.Bytes()
	if p == nil || len(p) < len(bs) {
		p = make([]byte, len(bs))
	}
	copy(p, bs)
	return len(bs), nil
}

func NewSelfMinerDO(prk *common.PrivateKey) (SelfMinerDO, error) {
	var mi SelfMinerDO

	keyBytes := prk.ExportKey()
	tempBuf := make([]byte, 32)
	if len(keyBytes) < 32 {
		copy(tempBuf[32-len(keyBytes):32], keyBytes[:])
	} else {
		copy(tempBuf[:], keyBytes[len(keyBytes)-32:])
	}
	mi.SecretSeed = base.RandFromBytes(tempBuf[:])
	mi.SK = *groupsig.NewSeckeyFromRand(mi.SecretSeed)
	mi.PK = *groupsig.NewPubkeyFromSeckey(mi.SK)
	mi.ID = groupsig.DeserializeID(prk.GetPubKey().GetAddress().Bytes())

	var err error
	mi.VrfPK, mi.VrfSK, err = base.VRFGenerateKey(&mi)
	return mi, err
}

func (mi SelfMinerDO) GetMinerID() groupsig.ID {
	return mi.ID
}

func (mi SelfMinerDO) GetSecret() base.Rand {
	return mi.SecretSeed
}

func (mi SelfMinerDO) GetDefaultSecKey() groupsig.Seckey {
	return mi.SK
}

func (mi SelfMinerDO) GetDefaultPubKey() groupsig.Pubkey {
	return mi.PK
}

func (mi SelfMinerDO) GenSecretForGroup(h common.Hash) base.Rand {
	r := base.RandFromBytes(h.Bytes())
	return mi.SecretSeed.DerivedRand(r[:])
}
