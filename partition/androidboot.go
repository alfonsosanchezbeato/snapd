// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package partition

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition/androidbootenv"
)

type androidboot struct{}

// newAndroidboot creates a new Androidboot bootloader object
func newAndroidBoot() Bootloader {
	a := &androidboot{}
	if !osutil.FileExists(a.ConfigFile()) {
		return nil
	}
	return a
}

func (a *androidboot) Name() string {
	return "androidboot"
}

func (a *androidboot) Dir() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/androidboot")
}

func (a *androidboot) ConfigFile() string {
	return filepath.Join(a.Dir(), "androidboot.env")
}

func (a *androidboot) GetBootVars(names ...string) (map[string]string, error) {
	env := androidbootenv.NewEnv(a.ConfigFile())
	if err := env.Load(); err != nil {
		return nil, err
	}

	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (a *androidboot) SetBootVars(values map[string]string) error {
	env := androidbootenv.NewEnv(a.ConfigFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}

func (a *androidboot) RebootForUpdate(afterMins int) error {
	// Write argument so we reboot to recovery partition. We use for this
	// the same file as systemd. See
	// https://github.com/systemd/systemd/blob/v229/src/basic/def.h#L44
	param := []byte("recovery\n")
	if err := ioutil.WriteFile(filepath.Join(dirs.SystemdRunDir,
		"reboot-param"), param, 0644); err != nil {
		return err
	}

	return reboot(afterMins)
}
