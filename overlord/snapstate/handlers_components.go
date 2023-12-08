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

package snapstate

import (
	"errors"
	"fmt"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
	"gopkg.in/tomb.v2"
)

// TaskComponentSetup returns the ComponentSetup with task params hold
// by or referred to by the task.
func TaskComponentSetup(t *state.Task) (*ComponentSetup, error) {
	var compSetup ComponentSetup

	err := t.Get("component-setup", &compSetup)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if err == nil {
		return &compSetup, nil
	}

	var id string
	err = t.Get("component-setup-task", &id)
	if err != nil {
		return nil, err
	}

	ts := t.State().Task(id)
	if ts == nil {
		return nil, fmt.Errorf("internal error: tasks are being pruned")
	}
	if err := ts.Get("component-setup", &compSetup); err != nil {
		return nil, err
	}
	return &compSetup, nil
}

func (m *SnapManager) doPrepareComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	compSetup, err := TaskComponentSetup(t)
	if err != nil {
		return err
	}

	if compSetup.Revision().Unset() {
		// This is a local installation, revision is -1 (there
		// is no history of local revisions for components).
		compSetup.CompSideInfo.Revision = snap.R(-1)
	}

	t.Set("component-setup", compSetup)
	return nil
}

func (m *SnapManager) doMountComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	perfTimings := state.TimingsForTask(t)
	compSetup, err := TaskComponentSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	// TODO we might want a checkComponents doing checks for some
	// component types (see checkSnap and checkSnapCallbacks slice)

	snapInstance := snap.InstanceName(compSetup.CompSideInfo.Component.SnapName,
		compSetup.SnapInstanceKey)
	compMntDir := snap.ComponentMountDir(compSetup.CompSideInfo.Component.ComponentName,
		snapInstance, compSetup.SnapRevision)
	cleanup := func() {
		st.Lock()
		defer st.Unlock()

		// RemoveComponentDir is idempotent so it's ok to always
		// call it in the cleanup path
		if err := m.backend.RemoveComponentDir(compMntDir); err != nil {
			t.Errorf("cannot cleanup partial setup component %q: %v",
				compSetup.CompSideInfo, err)
		}

	}

	pm := NewTaskProgressAdapterUnlocked(t)
	var installRecord *backend.InstallRecord
	timings.Run(perfTimings, "setup-component",
		fmt.Sprintf("setup component %q", compSetup.CompSideInfo.Component),
		func(timings.Measurer) {
			installRecord, err = m.backend.SetupComponent(
				compSetup.CompPath,
				compSetup.CompSideInfo,
				snapInstance,
				compSetup.SnapRevision,
				deviceCtx,
				pm)
		})
	if err != nil {
		cleanup()
		return err
	}

	// double check that the component is mounted
	var readInfoErr error
	for i := 0; i < 10; i++ {
		_, readInfoErr = snap.ReadComponentInfoFromMountPoint(compMntDir,
			compSetup.CompSideInfo)
		if readInfoErr == nil {
			logger.Debugf("component %q (%v) available at %q",
				compSetup.CompSideInfo.Component,
				compSetup.Revision(), compMntDir)
			break
		}
		if _, ok := readInfoErr.(*snap.NotFoundError); !ok {
			break
		}
		// snap not found, seems is not mounted yet
		msg := fmt.Sprintf("expected component %q rev %v to be mounted but is not",
			compSetup.CompSideInfo.Component, compSetup.Revision())
		readInfoErr = fmt.Errorf("cannot proceed, %s", msg)
		if i == 0 {
			logger.Noticef(msg)
		}
		time.Sleep(mountPollInterval)
	}
	if readInfoErr != nil {
		timings.Run(perfTimings, "undo-setup-component",
			fmt.Sprintf("Undo setup of component %q",
				compSetup.CompSideInfo.Component),
			func(timings.Measurer) {
				err = m.backend.UndoSetupComponent(compSetup.CompPath,
					compMntDir,
					installRecord, deviceCtx, pm)
			})
		if err != nil {
			st.Lock()
			t.Errorf("cannot undo partial setup of component %q: %v",
				compSetup.CompSideInfo.Component, err)
			st.Unlock()
		}

		cleanup()
		return readInfoErr
	}

	st.Lock()
	if installRecord != nil {
		t.Set("install-record", installRecord)
	}
	st.Unlock()

	st.Lock()
	perfTimings.Save(st)
	st.Unlock()

	return nil
}

func (m *SnapManager) undoMountComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	compSetup, err := TaskComponentSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	var installRecord backend.InstallRecord
	st.Lock()
	// install-record is optional
	err = t.Get("install-record", &installRecord)
	st.Unlock()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	snapInstance := snap.InstanceName(compSetup.CompSideInfo.Component.SnapName,
		compSetup.SnapInstanceKey)
	compMntDir := snap.ComponentMountDir(compSetup.CompSideInfo.Component.ComponentName,
		snapInstance, compSetup.SnapRevision)
	pm := NewTaskProgressAdapterUnlocked(t)
	if err := m.backend.UndoSetupComponent(compSetup.CompPath, compMntDir,
		&installRecord, deviceCtx, pm); err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	return m.backend.RemoveComponentDir(compMntDir)
}

// XXX empty stub
func (m *SnapManager) doNothingComponent(t *state.Task, _ *tomb.Tomb) error {
	return nil
}
