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

package snap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap/naming"
	"gopkg.in/yaml.v2"
)

// ComponentInfo is the content of a component.yaml file.
type ComponentInfo struct {
	Component   naming.ComponentRef `yaml:"component"`
	Type        ComponentType       `yaml:"type"`
	Version     string              `yaml:"version"`
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description"`
}

// ComponentSideInfo is the equivalent of SideInfo for components, and
// includes relevant information for which the canonical source is a
// snap store.
type ComponentSideInfo struct {
	Component naming.ComponentRef `json:"component"`
	Revision  Revision            `json:"revision"`
}

// ReadComponentInfoFromContainer reads ComponentInfo from a snap component container.
func ReadComponentInfoFromContainer(compf Container) (*ComponentInfo, error) {
	yamlData, err := compf.ReadFile("meta/component.yaml")
	if err != nil {
		return nil, err
	}

	var ci ComponentInfo

	if err := yaml.UnmarshalStrict(yamlData, &ci); err != nil {
		return nil, fmt.Errorf("cannot parse component.yaml: %s", err)
	}

	if err := ci.validate(); err != nil {
		return nil, err
	}

	return &ci, nil
}

func ReadComponentInfoFromMountPoint(mountPoint string, csi *ComponentSideInfo) (*ComponentInfo, error) {
	yamlFn := filepath.Join(mountPoint, "meta", "component.yaml")
	yamlData, err := ioutil.ReadFile(yamlFn)
	if os.IsNotExist(err) {
		return nil, &NotFoundError{Snap: csi.Component.String(),
			Revision: csi.Revision, Path: yamlFn}
	}
	if err != nil {
		return nil, err
	}

	var ci ComponentInfo

	if err := yaml.UnmarshalStrict(yamlData, &ci); err != nil {
		return nil, fmt.Errorf("cannot parse component.yaml: %s", err)
	}

	if err := ci.validate(); err != nil {
		return nil, err
	}

	return &ci, nil
}

// FullName returns the full name of the component, which is composed
// by snap name and component name.
func (ci *ComponentInfo) FullName() string {
	return ci.Component.String()
}

// ComponentMountDir returns the directory where a component gets mounted, which
// will be of the form:
// /snaps/<snap_instance>/components/<snap_revision>/<component_name>
func ComponentMountDir(compName, snapInstance string, snapRevision Revision) string {
	return filepath.Join(BaseDir(snapInstance), "components",
		snapRevision.String(), compName)
}

// MountDir returns the directory where the component gets mounted. It requires
// the instance name and revision of the owner to find the snap mount root dir.
func (ci *ComponentInfo) MountDir(snapInstance string, snapRevision Revision) string {
	return filepath.Join(BaseDir(snapInstance), "components",
		snapRevision.String(), ci.Component.ComponentName)
}

// MountFile returns the path of the file to be mounted for a component.
func (ci *ComponentInfo) MountFile(csi *ComponentSideInfo) string {
	return filepath.Join(dirs.SnapBlobDir,
		fmt.Sprintf("%s_%s.snap", ci.Component, csi.Revision))
}

// Validate performs some basic validations on component.yaml values.
func (ci *ComponentInfo) validate() error {
	if ci.Component.SnapName == "" {
		return fmt.Errorf("snap name for component cannot be empty")
	}
	if ci.Component.ComponentName == "" {
		return fmt.Errorf("component name cannot be empty")
	}
	if err := ci.Component.Validate(); err != nil {
		return err
	}
	if ci.Type == "" {
		return fmt.Errorf("component type cannot be empty")
	}
	// version is optional
	if ci.Version != "" {
		if err := ValidateVersion(ci.Version); err != nil {
			return err
		}
	}
	if err := ValidateSummary(ci.Summary); err != nil {
		return err
	}
	if err := ValidateDescription(ci.Description); err != nil {
		return err
	}
	return nil
}
