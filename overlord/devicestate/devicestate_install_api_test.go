// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package devicestate_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
	. "gopkg.in/check.v1"
)

type deviceMgrInstallAPISuite struct {
	deviceMgrBaseSuite
	*seedtest.TestingSeed20
}

var _ = Suite(&deviceMgrInstallAPISuite{})

func (s *deviceMgrInstallAPISuite) SetUpTest(c *C) {
	classic := true
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	// We uncompress a gadget with grub, and prefer not to mock in this case
	bootloader.Force(nil)

	restore := devicestate.MockSystemForPreseeding(func() (string, error) {
		return "fake system label", nil
	})
	defer restore()

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SeedDir = dirs.SnapSeedDir

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
}

func unpackSnap(snapBlob, targetDir string) error {
	out, err := exec.Command("unsquashfs", "-d", targetDir, "-f", snapBlob).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot unsquashfs: %v: %s", err, string(out))
	}
	return nil
}

func (s *deviceMgrInstallAPISuite) setupSystemSeed(c *C, sysLabel, gadgetYaml string, isClassic bool) *asserts.Model {
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["snapd"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc-kernel=22"],
		[][]string{{"kernel.efi", ""}}, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["core22"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc=22"],
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"pc-boot.img", ""}, {"pc-core.img", ""}, {"grubx64.efi", ""},
			{"shim.efi.signed", ""}, {"grub.conf", ""}},
		snap.R(1), "canonical", s.StoreSigning.Database)

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core22",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core22",
				"id":   s.AssertedSnapID("core22"),
				"type": "base",
			},
		},
	}
	if isClassic {
		model["classic"] = "true"
		model["distribution"] = "ubuntu"
	}

	return s.MakeSeed(c, sysLabel, "my-brand", "my-model", model, nil)
}

func (s *deviceMgrInstallAPISuite) TestInstallFinishNoEncryptionHappy(c *C) {
	encrypted := false
	isClassic := true

	// TODO UC case when supported
	restore := release.MockOnClassic(isClassic)
	defer restore()

	// Mock partitioned disk
	gadgetYaml := gadgettest.SingleVolumeClassicWithModesGadgetYaml
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	defer restore()

	// now create a system with snaps/assertions
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	label := "classic"
	model := s.setupSystemSeed(c, label, gadgetYaml, isClassic)
	c.Check(model, NotNil)

	// Create fake seed that will return information from the label we created
	// (TODO: needs to be in sync with setupSystemSeed, fix that)
	kernelSnapPath := filepath.Join(s.SeedDir, "snaps", "pc-kernel_1.snap")
	baseSnapPath := filepath.Join(s.SeedDir, "snaps", "core22_1.snap")
	gadgetSnapPath := filepath.Join(s.SeedDir, "snaps", "pc_1.snap")
	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{
				{
					Path:          kernelSnapPath,
					SideInfo:      &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("pc-kernel")},
					EssentialType: snap.TypeKernel,
				},
				{
					Path:          baseSnapPath,
					SideInfo:      &snap.SideInfo{RealName: "core22", Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("core22")},
					EssentialType: snap.TypeBase,
				},
				{
					Path:          gadgetSnapPath,
					SideInfo:      &snap.SideInfo{RealName: "pc", Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("pc")},
					EssentialType: snap.TypeGadget,
				},
			},
			model: model,
		}, nil
	})
	defer restore()

	// Mock calls to systemd-mount, which is used to mount snaps from the system label
	cmd := testutil.MockCommand(c, "systemd-mount", "")
	defer cmd.Restore()

	// Unpack gadget snap from seed where it would have been mounted
	gadgetDir := filepath.Join(dirs.SnapRunDir, "snap-content/gadget")
	err = os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = unpackSnap(filepath.Join(s.SeedDir, "snaps/pc_1.snap"), gadgetDir)
	c.Assert(err, IsNil)

	// Mock writing of contents
	writeContentCalls := 0
	restore = devicestate.MockInstallWriteContent(func(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *install.EncryptionSetupData, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error) {
		writeContentCalls++
		if encrypted {
			c.Check(encSetupData, NotNil)
		} else {
			c.Check(encSetupData, IsNil)
		}
		return nil, nil
	})
	defer restore()

	// Note that ESP must be mounted in the same place as a seed partition
	// so MarkRecoveryCapableSystem is happy when searching for a bootloader.
	// TODO Should this be changed?
	espDir := filepath.Join(dirs.RunDir, "mnt/ubuntu-seed")

	// Mock mounting of partitions
	mountVolsCalls := 0
	restore = devicestate.MockInstallMountVolumes(func(onVolumes map[string]*gadget.Volume, encSetupData *install.EncryptionSetupData) (espMntDir string, unmount func() error, err error) {
		mountVolsCalls++
		return espDir, func() error { return nil }, nil
	})
	s.state.Lock()
	defer s.state.Unlock()

	// Mock saving of traits
	saveStorageTraitsCalls := 0
	restore = devicestate.MockInstallSaveStorageTraits(func(model gadget.Model, allLaidOutVols map[string]*gadget.LaidOutVolume, encryptSetupData *install.EncryptionSetupData) error {
		saveStorageTraitsCalls++
		return nil
	})

	chg := s.state.NewChange("install-step-finish", "finish setup of run system")
	finishTask := s.state.NewTask("install-finish", "install API finish step")
	finishTask.Set("system-label", label)
	finishTask.Set("on-volumes", ginfo.Volumes)
	chg.AddTask(finishTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	s.state.Unlock()

	// Check now
	kernelDir := filepath.Join(dirs.SnapRunDir, "snap-content/kernel")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", gadgetSnapPath, gadgetDir},
		{"systemd-mount", kernelSnapPath, kernelDir},
		{"systemd-mount", "--umount", gadgetDir},
		{"systemd-mount", "--umount", kernelDir},
	})
	c.Check(writeContentCalls, Equals, 1)
	c.Check(mountVolsCalls, Equals, 1)
	c.Check(saveStorageTraitsCalls, Equals, 1)

	expectedFiles := []string{
		filepath.Join(espDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(espDir, "EFI/ubuntu/grubenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/grub.cfg"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/grubenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/pc-kernel_1.snap/kernel.efi"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/kernel.efi"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/device/model"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/modeenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/snaps/core22_1.snap"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/snaps/pc_1.snap"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/snaps/pc-kernel_1.snap"),
	}
	for _, f := range expectedFiles {
		_, err := ioutil.ReadFile(f)
		c.Check(err, IsNil)
	}
}
