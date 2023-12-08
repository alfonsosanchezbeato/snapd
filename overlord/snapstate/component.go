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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"errors"
	"fmt"
	"os"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// InstallComponentPath returns a set of tasks for installing a snap component
// from a file path and the snap.ComponentInfo for the component.
//
// Note that the state must be locked by the caller. The provided SideInfo can
// contain just a name which results in local sideloading of the component, or
// full metadata in which case the component will appear as installed from the
// store.
func InstallComponentPath(st *state.State, si *snap.SideInfo, compInfo *snap.ComponentInfo,
	path, instanceName, channel string, flags Flags) (*state.TaskSet, error) {
	if si.RealName == "" {
		return nil, fmt.Errorf(
			"internal error: snap name to install component %q not provided",
			path)
	}

	if instanceName == "" {
		instanceName = si.RealName
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, err
	}

	var snapst SnapState
	err = Get(st, instanceName, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	channel, err = resolveChannel(instanceName, snapst.TrackingChannel, channel,
		deviceCtx)
	if err != nil {
		return nil, err
	}

	// TODO check compInfo against SideInfo
	// Check snap name matches
	if compInfo.Component.SnapName != si.RealName {
		return nil, fmt.Errorf(
			"component snap name %q does not match real snap name %q",
			compInfo.Component.SnapName, si.RealName)
	}

	// Read snap.Info from the installed snap
	info, err := readInfo(instanceName, si, withAuxStoreInfo)
	if err != nil {
		return nil, err
	}

	providerContentAttrs := defaultProviderContentAttrs(st, info, nil)
	snapsup := &SnapSetup{
		Base:               info.Base,
		Prereq:             getKeys(providerContentAttrs),
		PrereqContentAttrs: providerContentAttrs,
		SideInfo:           si,
		SnapPath:           path,
		Channel:            channel,
		Flags:              flags.ForSnapSetup(),
		Type:               info.Type(),
		Version:            info.Version,
		PlugsOnly:          len(info.Slots) == 0,
		InstanceKey:        info.InstanceKey,
	}

	compSetup := &ComponentSetup{
		// TODO component revision is unset for the moment (local component)
		CompSideInfo:    &snap.ComponentSideInfo{Component: compInfo.Component},
		CompPath:        path,
		SnapInstanceKey: info.InstanceKey,
		SnapType:        info.Type(),
		SnapRevision:    info.Revision,
	}
	removeSnapPath := true
	return doInstallComponent(st, &snapst, snapsup, compSetup, path, removeSnapPath, 0, "")
}

// TODO
// doRefreshComponent

// doInstallComponent assumes that the owner snap is already installed.
func doInstallComponent(st *state.State, snapst *SnapState, snapsup *SnapSetup,
	compSetup *ComponentSetup, path string, removeSnapPath bool, flags int,
	fromChange string) (*state.TaskSet, error) {

	// TODO check for experimental flag to hide temporarily components

	snapSi := snapsup.SideInfo
	compSi := compSetup.CompSideInfo

	if snapst.IsInstalled() && !snapst.Active {
		return nil, fmt.Errorf("cannot install component %q for disabled snap %q",
			compSi.Component, snapSi.RealName)
	}

	if err := isParallelInstallable(snapsup); err != nil {
		return nil, err
	}

	// TODO extend conflict checks to components, this will check only for
	// snaps conflicts.
	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(),
		snapst, fromChange); err != nil {
		return nil, err
	}

	// check if we already have the revision in the snaps folder (alters tasks)
	revisionIsPresent := snapst.IsComponentRevInstalled(snapSi, compSi) == true
	revisionStr := fmt.Sprintf(" (%s)", compSi.Revision)

	// XXX different task?
	var prepare, prev *state.Task
	// if we have a local revision here we go back to that
	if path != "" || revisionIsPresent {
		prepare = st.NewTask("prepare-component",
			fmt.Sprintf(i18n.G("Prepare component %q%s"),
				path, revisionStr))
	} else {
		// TODO this task type needs to be implemented yet
		prepare = st.NewTask("download-component",
			fmt.Sprintf(i18n.G("Download component %q%s"),
				compSi.Component, revisionStr))
	}
	prepare.Set("component-setup", compSetup)

	tasks := []*state.Task{prepare}
	prev = prepare

	addTask := func(t *state.Task) {
		t.Set("component-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
	}

	// TODO task to fetch and check assertions for component

	// mount
	if !revisionIsPresent {
		mount := st.NewTask("mount-component",
			fmt.Sprintf(i18n.G("Mount component %q%s"),
				compSi.Component, revisionStr))
		addTask(mount)
		prev = mount
	} else {
		if removeSnapPath {
			// If the revision is local, we will not need the
			// temporary snap. This can happen when e.g.
			// side-loading a local revision again. The path is
			// only needed in the "mount-snap" handler and that is
			// skipped for local revisions.
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		}
	}

	// TODO hooks for components

	// We might be replacing a component if a local install, otherwise
	// this is not really possible.
	compInstalled := snapst.IsComponentInstalled(compSi.Component)
	if compInstalled {
		unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G(
			"Make current revision for component %q unavailable"),
			compSi.Component))
		addTask(unlink)
		prev = unlink
	}

	// finalize (wrappers+current symlink)
	linkSnap := st.NewTask("link-component",
		fmt.Sprintf(i18n.G("Make component %q%s available to the system"),
			compSi.Component, revisionStr))
	addTask(linkSnap)
	prev = linkSnap

	installSet := state.NewTaskSet(tasks...)
	installSet.MarkEdge(prepare, BeginEdge)
	installSet.MarkEdge(linkSnap, MaybeRebootEdge)

	// TODO if snap is being installed from the store, then the last task before
	// any system modifications are done is check validate-snap, otherwise
	// it's the prepare-component
	installSet.MarkEdge(prepare, LastBeforeLocalModificationsEdge)
	if flags&noRestartBoundaries == 0 {
		if err := SetEssentialSnapsRestartBoundaries(st, nil,
			[]*state.TaskSet{installSet}); err != nil {
			return nil, err
		}
	}

	return installSet, nil
}
