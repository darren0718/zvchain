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

package logical

import (
	"github.com/darren0718/zvchain/common"
	"github.com/darren0718/zvchain/middleware/types"
	"math/rand"
	"testing"
)

func TestSkipCounts(t *testing.T) {
	ret := make(skipCounts)

	ret.addCount(common.HexToHash("0x1"), 3)
	c := ret.count(common.HexToHash("0x1"))
	if c != 3 {
		t.Fatal("count error")
	}
}

type activatedGroupReader4Test struct {
	groups []types.GroupI
}

func (r *activatedGroupReader4Test) GetActivatedGroupsAt(height uint64) []types.GroupI {
	gs := make([]types.GroupI, 0)
	for _, g := range r.groups {
		if g.Header().WorkHeight() <= height && g.Header().DismissHeight() > height {
			gs = append(gs, g)
		}
	}
	return gs
}

func (r *activatedGroupReader4Test) GetGroupSkipCountsAt(h uint64, groups []types.GroupI) (map[common.Hash]uint16, error) {
	skip := make(skipCounts)
	for i, g := range r.groups {
		if i < 4 {
			skip.addCount(g.Header().Seed(), uint16(2*i))
		}
	}
	return skip, nil
}

func newActivatedGroupReader4Test() *activatedGroupReader4Test {
	return &activatedGroupReader4Test{
		groups: make([]types.GroupI, 0),
	}
}

func (r *activatedGroupReader4Test) init() {
	for h := uint64(0); h < 10000; h += 10 {
		gh := newGroupHeader4Test(h, h+200)
		g := &group4Test{header: gh}
		r.groups = append(r.groups, g)
	}
}

func buildGroupSelector4Test() *groupSelector {
	gr := newActivatedGroupReader4Test()
	gr.init()
	return newGroupSelector(gr)
}

func TestGroupSelector_getWorkGroupSeedsAt(t *testing.T) {
	gs := buildGroupSelector4Test()
	rnd := make([]byte, 32)
	rand.Read(rnd)
	bh := &types.BlockHeader{
		Height: 100,
		Random: rnd,
	}
	bh.Hash = bh.GenHash()
	seeds := gs.getWorkGroupSeedsAt(bh, 102)
	t.Log(seeds)
}

func TestGroupSelector_doSelect(t *testing.T) {
	gs := buildGroupSelector4Test()
	rnd := make([]byte, 32)
	rand.Read(rnd)
	bh := &types.BlockHeader{
		Height: 100,
		Random: rnd,
	}
	bh.Hash = bh.GenHash()

	for h := bh.Height + 1; h < 1000; h++ {
		selected := gs.doSelect(bh, h)
		if selected == common.EmptyHash {

		}
	}
}

func groupSkipCountsBetween2(gs *groupSelector, preBH *types.BlockHeader, height uint64) skipCounts {
	sc := make(skipCounts)
	h := preBH.Height + 1
	for ; h < height; h++ {
		expectedSeed := gs.doSelect(preBH, h)
		sc.addCount(expectedSeed, 1)
	}
	return sc
}

func TestGroupSelector_groupSkipCountsBetween(t *testing.T) {
	gs := buildGroupSelector4Test()
	rnd := make([]byte, 32)
	rand.Read(rnd)
	bh := &types.BlockHeader{
		Height: 200,
		Random: rnd,
	}
	bh.Hash = bh.GenHash()

	for h := bh.Height + 1; h < 300; h++ {
		ret1 := gs.groupSkipCountsBetween(bh, h)
		ret2 := groupSkipCountsBetween2(gs, bh, h)
		for gseed, cnt := range ret1 {
			if cnt != ret2.count(gseed) {
				t.Errorf("calc error at %v", h)
			}
		}
	}
}

func Benchmark_groupSkipCountsBetween2(b *testing.B) {
	gs := buildGroupSelector4Test()
	rnd := make([]byte, 32)
	rand.Read(rnd)
	bh := &types.BlockHeader{
		Height: 100,
		Random: rnd,
	}
	bh.Hash = bh.GenHash()
	for i := 0; i < b.N; i++ {
		groupSkipCountsBetween2(gs, bh, 5000)
	}
}

func BenchmarkGroupSelector_groupSkipCountsBetween(b *testing.B) {
	gs := buildGroupSelector4Test()
	rnd := make([]byte, 32)
	rand.Read(rnd)
	bh := &types.BlockHeader{
		Height: 100,
		Random: rnd,
	}
	bh.Hash = bh.GenHash()
	for i := 0; i < b.N; i++ {
		gs.groupSkipCountsBetween(bh, 10000)

	}
}
