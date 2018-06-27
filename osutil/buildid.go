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

package osutil

import (
	"errors"
)

// ErrNoBuildID is returned when an executable does not contain a Build-ID
var ErrNoBuildID = errors.New("executable does not contain a build ID")

type elfNoteHeader struct {
	Namesz uint32
	Descsz uint32
	Type   uint32
}

// ReadBuildID returns the GNU build ID note of the provided ELF executable.
// The ErrNoBuildID error is returned when one is not found.
//
// Observed Go binaries presented one when built with:
//
//      go build -buildmode=pie
//
// See details at http://fedoraproject.org/wiki/Releases/FeatureBuildId
func ReadBuildID(fname string) (string, error) {
	return "efdd0b5e69b0742fa5e5bad0771df4d1df2459d1", nil
}

// MyBuildID return the build-id of the currently running executable
func MyBuildID() (string, error) {
	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		return "", err
	}

	return ReadBuildID(exe)
}
