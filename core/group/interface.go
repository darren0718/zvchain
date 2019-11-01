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

package group

import (
	"bytes"
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/middleware/types"
)

type minerPunishment interface {
	MinerFrozen(accountDB types.AccountDB, miner common.Address, height uint64) (success bool, err error)
	MinerPenalty(accountDB types.AccountDB, penalty types.PunishmentMsg, height uint64) (success bool, err error)
}

type chainReader interface {
	Height() uint64
	LatestAccountDB() (types.AccountDB, error)
	MinerSk() string
	AddTransactionToPool(tx *types.Transaction) (bool, error)
	AccountDBAt(height uint64) (types.AccountDB, error)
	QueryBlockHeaderByHash(hash common.Hash) *types.BlockHeader
}

// Round 1 tx data,implement common.EncryptedSharePiecePacket
type EncryptedSharePiecePacketImpl struct {
	SeedD    common.Hash `msgpack:"se"`           // Seed
	SenderD  []byte      `msgpack:"sr,omitempty"` // sender's address. will set from transaction source
	Pubkey0D []byte      `msgpack:"pb"`           // the gpk share of the miner
	PiecesD  []byte      `msgpack:"pi"`           // array of encrypted piece for every group member
}

func (e *EncryptedSharePiecePacketImpl) Seed() common.Hash {
	return e.SeedD
}

func (e *EncryptedSharePiecePacketImpl) Sender() []byte {
	return e.SenderD
}

func (e *EncryptedSharePiecePacketImpl) Pieces() []byte {
	return e.PiecesD
}

func (e *EncryptedSharePiecePacketImpl) Pubkey0() []byte {
	return e.Pubkey0D
}

// Round 2 tx data. implement interface types.MpkPacket
type MpkPacketImpl struct {
	SeedD   common.Hash `msgpack:"se"`           // Seed
	SenderD []byte      `msgpack:"sr,omitempty"` // sender's address
	MpkD    []byte      `msgpack:"mp"`           // mpk
	SignD   []byte      `msgpack:"si"`           // byte data of Seed signed by mpk
}

func (s *MpkPacketImpl) Seed() common.Hash {
	return s.SeedD
}

func (s *MpkPacketImpl) Sender() []byte {
	return s.SenderD
}

func (s *MpkPacketImpl) Mpk() []byte {
	return s.MpkD
}

func (s *MpkPacketImpl) Sign() []byte {
	return s.SignD
}

// OriginSharePiecePacket implements types.OriginSharePiecePacket.
type OriginSharePiecePacketImpl struct {
	SeedD      common.Hash `msgpack:"se"`           // Seed
	SenderD    []byte      `msgpack:"sr,omitempty"` // sender's address. will set from transaction source
	EncSeckeyD []byte      `msgpack:"es"`           // the gpk share of the miner
	PiecesD    []byte      `msgpack:"pi"`           // array of origin piece for every group member
}

func (e *OriginSharePiecePacketImpl) Seed() common.Hash {
	return e.SeedD
}

func (e *OriginSharePiecePacketImpl) Sender() []byte {
	return e.SenderD
}

func (e *OriginSharePiecePacketImpl) Pieces() []byte {
	return e.PiecesD
}

func (e *OriginSharePiecePacketImpl) EncSeckey() []byte {
	return e.EncSeckeyD
}

type FullPacketImpl struct {
	mpks   []types.MpkPacket
	pieces []types.EncryptedSharePiecePacket
}

func (s *FullPacketImpl) Mpks() []types.MpkPacket {
	return s.mpks
}

func (s *FullPacketImpl) Pieces() []types.EncryptedSharePiecePacket {
	return s.pieces
}

// group implements the types.GroupI
type group struct {
	HeaderD  *groupHeader
	MembersD []*member
}

func (g *group) Header() types.GroupHeaderI {
	return g.HeaderD
}

func (g *group) Members() []types.MemberI {
	rs := make([]types.MemberI, len(g.MembersD))
	for k, v := range g.MembersD {
		rs[k] = v
	}
	return rs
}

func (g *group) hasMember(id []byte) bool {
	for _, mem := range g.Members() {
		if bytes.Equal(mem.ID(), id) {
			return true
		}
	}
	return false
}

type member struct {
	Id []byte
	Pk []byte
}

func (m *member) ID() []byte {
	return m.Id
}

func (m *member) PK() []byte {
	return m.Pk
}

// groupHeader implements the types.GroupHeaderI
type groupHeader struct {
	SeedD          common.Hash `msgpack:"se"` // seed of current group, unique
	WorkHeightD    uint64      `msgpack:"wh"` // the block height of group start to work
	DismissHeightD uint64      `msgpack:"dh"` // the block height of group dismiss
	PublicKeyD     []byte      `msgpack:"pd"` // group's public key
	ThresholdD     uint32      `msgpack:"th"` // the threshold number to validate a block for this group
	PreSeed        common.Hash `msgpack:"ps"` // seed of pre group
	GroupHeightD   uint64      `msgpack:"gh"` // group height
}

func (g *groupHeader) Seed() common.Hash {
	return g.SeedD
}

func (g *groupHeader) WorkHeight() uint64 {
	return g.WorkHeightD
}

func (g *groupHeader) DismissHeight() uint64 {
	return g.DismissHeightD
}

func (g *groupHeader) PublicKey() []byte {
	return g.PublicKeyD
}
func (g *groupHeader) Threshold() uint32 {
	return g.ThresholdD
}
func (g *groupHeader) GroupHeight() uint64 {
	return g.GroupHeightD
}
func (g *groupHeader) livedAt(height uint64) bool {
	return g.DismissHeight() > height
}

func (g *groupHeader) activatedAt(height uint64) bool {
	return g.WorkHeight() <= height && g.livedAt(height)
}

func newGroup(i types.GroupI, top *group) *group {
	var (
		preSeed        = common.EmptyHash
		gh      uint64 = 0
	)
	if top != nil {
		preSeed = top.HeaderD.SeedD
		gh = top.HeaderD.GroupHeight() + 1
	}
	header := &groupHeader{
		SeedD:          i.Header().Seed(),
		WorkHeightD:    i.Header().WorkHeight(),
		DismissHeightD: i.Header().DismissHeight(),
		PublicKeyD:     i.Header().PublicKey(),
		ThresholdD:     i.Header().Threshold(),
		PreSeed:        preSeed,
		GroupHeightD:   gh,
	}
	members := make([]*member, 0)
	for _, m := range i.Members() {
		mem := &member{m.ID(), m.PK()}
		members = append(members, mem)
	}
	return &group{header, members}
}

type groupSkipCounter interface {
	GroupSkipCountsBetween(preBH *types.BlockHeader, height uint64) map[common.Hash]uint16
}
