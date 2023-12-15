// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

type linkCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&linkCompSnapSuite{})

var taskRunTime time.Time

func (s *linkCompSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	var err error
	taskRunTime, err = time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	c.Assert(err, IsNil)
	s.AddCleanup(snapstate.MockTimeNow(func() time.Time {
		return taskRunTime
	}))
}

func (s *linkCompSnapSuite) testDoLinkComponent(c *C, snapName string, snapRev snap.Revision) {
	const compName = "mycomp"
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask("link-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(chg.Err(), IsNil)
	// undo information has been stored
	var storedCsi snap.ComponentSideInfo
	t.Get("linked-component", &storedCsi)
	c.Assert(&storedCsi, DeepEquals, csi)
	// state is modified as expected
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	stateCsi := snapst.CurrentComponentSideInfo(cref)
	c.Assert(stateCsi, DeepEquals, csi)
	c.Assert(snapst.LastCompRefreshTime[csi.Component.ComponentName], Equals, taskRunTime)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoLinkComponent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state does not contain the component
	setStateWithOneSnap(s.state, snapName, snapRev)
	s.state.Unlock()

	s.testDoLinkComponent(c, snapName, snapRev)
}

func (s *linkCompSnapSuite) TestDoLinkComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state with some component around already
	setStateWithOneComponent(s.state, snapName, snapRev, "other-comp", snap.R(33))
	s.state.Unlock()

	s.testDoLinkComponent(c, snapName, snapRev)
}

func (s *linkCompSnapSuite) testDoLinkThenUndoLinkComponent(c *C, snapName string, snapRev snap.Revision) {
	const compName = "mycomp"
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	// state does not contain the component
	setStateWithOneSnap(s.state, snapName, snapRev)

	t := s.state.NewTask("link-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo link")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo link (error out)")
	// undo information was stored
	var storedCsi snap.ComponentSideInfo
	t.Get("linked-component", &storedCsi)
	c.Assert(&storedCsi, DeepEquals, csi)
	// the component is not in the state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	c.Assert(snapst.CurrentComponentSideInfo(cref), IsNil)
	c.Assert(snapst.LastCompRefreshTime[csi.Component.ComponentName], Equals, taskRunTime)
	c.Assert(t.Status(), Equals, state.UndoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoLinkThenUndoLinkComponent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state does not contain the component
	setStateWithOneSnap(s.state, snapName, snapRev)
	s.state.Unlock()

	s.testDoLinkThenUndoLinkComponent(c, snapName, snapRev)
}

func (s *linkCompSnapSuite) TestDoLinkThenUndoLinkComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state with some component around already
	setStateWithOneComponent(s.state, snapName, snapRev, "other-comp", snap.R(33))
	s.state.Unlock()

	s.testDoLinkThenUndoLinkComponent(c, snapName, snapRev)
}

func (s *linkCompSnapSuite) testDoUnlinkCurrentComponent(c *C, snapName string, snapRev snap.Revision, compName string, compRev snap.Revision) {
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask("unlink-current-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(chg.Err(), IsNil)
	// undo information has been stored
	var storedCsi snap.ComponentSideInfo
	t.Get("unlinked-component", &storedCsi)
	c.Assert(&storedCsi, DeepEquals, csi)
	// state is modified as expected
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	c.Assert(snapst.CurrentComponentSideInfo(cref), IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoUnlinkCurrentComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)
	s.state.Unlock()

	s.testDoUnlinkCurrentComponent(c, snapName, snapRev, compName, compRev)
}

func (s *linkCompSnapSuite) TestDoUnlinkCurrentComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component, we add some additional components as well
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	setStateWithComponents(s.state, snapName, snapRev, []*snap.ComponentSideInfo{csi1, csi2})
	s.state.Unlock()

	s.testDoUnlinkCurrentComponent(c, snapName, snapRev, compName, compRev)
}

func (s *linkCompSnapSuite) testDoUnlinkThenUndoUnlinkCurrentComponent(c *C, snapName string, snapRev snap.Revision, compName string, compRev snap.Revision) {
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask("unlink-current-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo link")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo link (error out)")
	// undo information was stored
	var storedCsi snap.ComponentSideInfo
	t.Get("unlinked-component", &storedCsi)
	c.Assert(&storedCsi, DeepEquals, csi)
	// the component is still in the state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	stateCsi := snapst.CurrentComponentSideInfo(cref)
	c.Assert(stateCsi, DeepEquals, csi)
	c.Assert(t.Status(), Equals, state.UndoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoUnlinkThenUndoUnlinkCurrentComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)
	s.state.Unlock()

	s.testDoUnlinkThenUndoUnlinkCurrentComponent(c, snapName, snapRev, compName, compRev)
}

func (s *linkCompSnapSuite) TestDoUnlinkThenUndoUnlinkCurrentComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component, we add some additional components as well
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	setStateWithComponents(s.state, snapName, snapRev, []*snap.ComponentSideInfo{csi1, csi2})
	s.state.Unlock()

	s.testDoUnlinkThenUndoUnlinkCurrentComponent(c, snapName, snapRev, compName, compRev)
}
