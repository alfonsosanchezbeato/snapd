// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snapstate_test

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

type discardCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&discardCompSnapSuite{})

func (s *discardCompSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *discardCompSnapSuite) TestDoDiscardComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	ci, compPath := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()

	t := s.state.NewTask("discard-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	compsup := snapstate.NewComponentSetup(csi, snap.TestComponent, compPath)
	t.Set("component-setup", compsup)
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	compDiscardRev := snap.R(5)
	csiToDiscard := snap.NewComponentSideInfo(cref, compDiscardRev)
	cs := sequence.NewComponentState(csiToDiscard, snap.TestComponent)
	chg.Set(fmt.Sprintf("unlinked-component-%s", cs.SideInfo.Component.String()), cs)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:                "undo-setup-component",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_5.comp",
		},
		{
			op:                "remove-component-dir",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_5.comp",
		},
	})
}

func (s *discardCompSnapSuite) TestDoDiscardComponentNoUnlinkedComp(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	ci, compPath := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()

	t := s.state.NewTask("discard-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	compsup := snapstate.NewComponentSetup(csi, snap.TestComponent, compPath)
	t.Set("component-setup", compsup)
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	// No unlinked component in the change

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- task desc (no state entry for key \"unlinked-component-mysnap+mycomp\")")
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, IsNil)
}
