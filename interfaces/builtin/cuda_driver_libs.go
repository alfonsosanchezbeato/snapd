// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package builtin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/snap"
)

const cudaDriverLibsSummary = `allows exposing CUDA driver libraries to the system or snaps`

const cudaDriverLibsBaseDeclarationPlugs = `
  cuda-driver-libs:
    allow-installation:
      plug-snap-type:
        - core
    allow-auto-connection:
      slots-per-plug: *
`

const cudaDriverLibsBaseDeclarationSlots = `
  cuda-driver-libs:
    allow-installation: false
    deny-auto-connection: true
`

var dirLibsAttrTypeError = errors.New(`cuda-driver-libs "source" attribute must be a list`)

// cudaDriverLibsInterface allows exposing CUDA driver libraries to the system or snaps.
type cudaDriverLibsInterface struct {
	commonInterface
}

func (iface *cudaDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	libDirs := []string{}
	if err := slot.Attr("source", &libDirs); err != nil {
		return err
	}
	// Validate directories
	for _, dir := range libDirs {
		if !strings.HasPrefix(dir, "$SNAP") && !strings.HasPrefix(dir, "${SNAP}") {
			return fmt.Errorf("cuda-driver-libs source directory %q must start with $SNAP or ${SNAP}", dir)
		}
	}

	return nil
}

func (iface *cudaDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("source", &libDirs); err != nil {
		return err
	}
	expandedDirs := make([]string, 0, len(libDirs))
	for _, dir := range libDirs {
		expandedDirs = append(expandedDirs, slot.Snap().ExpandSnapVariables(dir))
	}
	spec.AddLibDirs(expandedDirs)
	return nil
}

func (iface *cudaDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&cudaDriverLibsInterface{
		commonInterface: commonInterface{
			name:                  "cuda-driver-libs",
			summary:               cudaDriverLibsSummary,
			baseDeclarationPlugs:  cudaDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots:  cudaDriverLibsBaseDeclarationSlots,
			implicitPlugOnCore:    true,
			implicitPlugOnClassic: true,
		},
	})
}
