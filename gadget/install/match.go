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

package install

import (
	"fmt"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
)

type MatchedVolume struct {
	gadgetVol *gadget.Volume
	onDiskVol *gadget.OnDiskVolume
}

func findAllOnDiskVolumes() ([]*gadget.OnDiskVolume, error) {
	// Would it ever make sense to also look at virtual block devices?
	blockdevDisks, err := disks.AllPhysicalDisks()
	if err != nil {
		return nil, fmt.Errorf("while retrieving physical disks data: %v", err)
	}

	vols := []*gadget.OnDiskVolume{}
	for _, d := range blockdevDisks {
		v, err := gadget.OnDiskVolumeFromDisk(d)
		if err != nil {
			return nil, fmt.Errorf("while retrieving disk data: %v", err)
		}
		vols = append(vols, v)
	}
	return vols, nil
}

func onDiskMatchesGadgetVolume(dV *gadget.OnDiskVolume, gV *gadget.Volume,
	opts *gadget.VolumeCompatibilityOptions) bool {
	if err := gadget.EnsureVolumeCompatibility(gV, dV, opts); err != nil {
		logger.Noticef("%q disk is incompatible with gadget volume %q: %v",
			dV.Device, gV.Name, err)
		return false
	}
	return true
}

// FindDisksForGadgetVolumes returns a slice of gadget volumes matched to system
// disks. The input gVols is a map of gadget volumes and options used while comparing
// with the disks.
func FindDisksForGadgetVolumes(gVols map[string]*gadget.Volume,
	opts *gadget.VolumeCompatibilityOptions) ([]*MatchedVolume, error) {

	dVols, err := findAllOnDiskVolumes()
	if err != nil {
		return nil, err
	}

	type onDiskSearchInfo struct {
		onDiskV *gadget.OnDiskVolume
		matched bool
	}
	diskSearchInfo := []onDiskSearchInfo{}
	for _, onDiskV := range dVols {
		diskSearchInfo = append(diskSearchInfo, onDiskSearchInfo{onDiskV: onDiskV})
	}

	matched := []*MatchedVolume{}
	for _, gV := range gVols {
		gVolMatched := false
		for _, dsi := range diskSearchInfo {
			if dsi.matched {
				continue
			}
			if onDiskMatchesGadgetVolume(dsi.onDiskV, gV, opts) {
				matched = append(matched, &MatchedVolume{gV, dsi.onDiskV})
				gVolMatched = true
				dsi.matched = true
				break
			}
		}
		if !gVolMatched {
			return nil, fmt.Errorf("no disk matches gadget volume %q", gV.Name)
		}
	}

	return matched, nil
}
