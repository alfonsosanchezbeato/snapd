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

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/mkfs"
)

// emptyFixedBlockDevices finds any non-removalble physical disk that has
// no partitions. It will exclude loop devices.
func emptyFixedBlockDevices() (devices []string, err error) {
	// eg. /sys/block/sda/removable
	removable, err := filepath.Glob(filepath.Join(dirs.GlobalRootDir, "/sys/block/*/removable"))
	if err != nil {
		return nil, err
	}
	for _, removableAttr := range removable {
		val, err := ioutil.ReadFile(removableAttr)
		if err != nil || string(val) != "0\n" {
			// removable, ignore
			continue
		}
		// let's see if it has partitions
		dev := filepath.Base(filepath.Dir(removableAttr))
		if strings.HasPrefix(dev, "loop") {
			// is loop device, ignore
			continue
		}
		pattern := fmt.Sprintf(filepath.Join(dirs.GlobalRootDir, "/sys/block/%s/%s*/partition"), dev, dev)
		// eg. /sys/block/sda/sda1/partition
		partitionAttrs, _ := filepath.Glob(pattern)
		if len(partitionAttrs) != 0 {
			// has partitions, ignore
			continue
		}
		devNode := fmt.Sprintf("/dev/%s", dev)
		output, err := exec.Command("lsblk", "--output", "fstype", "--json", devNode).CombinedOutput()
		if err != nil {
			return nil, osutil.OutputErr(output, err)
		}
		// TODO: parser proper json
		if !strings.Contains(string(output), "null") {
			// found a filesystem, ignore
			continue
		}

		devices = append(devices, devNode)
	}
	sort.Strings(devices)
	return devices, nil
}

func firstVol(volumes map[string]*gadget.Volume) *gadget.Volume {
	for _, vol := range volumes {
		return vol
	}
	return nil
}

func createPartitions(bootDevice string, volumes map[string]*gadget.Volume) ([]gadget.OnDiskStructure, error) {
	if len(volumes) != 1 {
		return nil, fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}
	// TODO: support multiple volumes, see gadget/install/install.go
	if len(diskLayout.Structure) > 0 {
		return nil, fmt.Errorf("cannot yet install on a disk that has partitions")
	}

	layoutOpts := &gadget.LayoutOptions{
		IgnoreContent: true,
	}

	vol := firstVol(volumes)
	lvol, err := gadget.LayoutVolume(vol, gadget.DefaultConstraints, layoutOpts)
	if err != nil {
		return nil, fmt.Errorf("cannot layout volume: %v", err)
	}

	iconst := &install.CreateOptions{CreateAllMissingPartitions: true}
	created, err := install.CreateMissingPartitions(diskLayout, lvol, iconst)
	if err != nil {
		return nil, fmt.Errorf("cannot create parititons: %v", err)
	}
	logger.Noticef("created %v partitions", created)

	return created, nil
}

func runMntFor(label string) string {
	// TODO: use a different location than snapd here but right now
	//       snapd expects things to be mounted in the "finish" step
	return filepath.Join(dirs.GlobalRootDir, "/run/mnt/", label)
}

func postSystemsInstallSetupStorageEncryption(details *client.SystemDetails) error {
	// TODO: check details.StorageEncryption and call POST
	// /systems/<seed-label> with "action":"install" and
	// "step":"setup-storage-encryption"
	return nil
}

// XXX: reuse/extract cmd/snap/wait.go:waitMixin()
func waitChange(chgId string) error {
	cli := client.New(nil)
	for {
		chg, err := cli.Change(chgId)
		if err != nil {
			return err
		}

		if chg.Err != "" {
			return errors.New(chg.Err)
		}
		if chg.Ready {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

// TODO laidoutStructs is used to get the devices, when encryption is
// happening maybe we need to find the information differently.
func postSystemsInstallFinish(cli *client.Client,
	details *client.SystemDetails, bootDevice string,
	laidoutStructs []gadget.OnDiskStructure) error {

	vols := make(map[string]*gadget.Volume)
	for volName, gadgetVol := range details.Volumes {
		laidIdx := 0
		for i := range gadgetVol.Structure {
			// TODO mbr is special, what is the device for that?
			var device string
			if gadgetVol.Structure[i].Role == "mbr" {
				device = bootDevice
			} else {
				device = laidoutStructs[laidIdx].Node
				laidIdx++
			}
			gadgetVol.Structure[i].Device = device
		}
		vols[volName] = gadgetVol
	}

	// Finish steps does the writing of assets
	opts := &client.InstallSystemOptions{
		Step:      client.InstallStepFinish,
		OnVolumes: vols,
	}
	chgId, err := cli.InstallSystem(details.Label, opts)
	if err != nil {
		return err
	}
	fmt.Printf("Change %s created\n", chgId)
	return waitChange(chgId)
}

// createAndMountFilesystems creates and mounts filesystems. It returns
// an slice with the paths where the filesystems have been mounted to.
func createAndMountFilesystems(bootDevice string, volumes map[string]*gadget.Volume) ([]string, error) {
	// only support a single volume for now
	if len(volumes) != 1 {
		return nil, fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	disk, err := disks.DiskFromDeviceName(bootDevice)
	if err != nil {
		return nil, err
	}
	vol := firstVol(volumes)

	var mountPoints []string
	for _, stru := range vol.Structure {
		if stru.Label == "" || stru.Filesystem == "" {
			continue
		}

		part, err := disk.FindMatchingPartitionWithPartLabel(stru.Label)
		if err != nil {
			return nil, err
		}
		// XXX: reuse
		// gadget/install/content.go:mountFilesystem() instead
		// (it will also call udevadm)
		if err := mkfs.Make(stru.Filesystem, part.KernelDeviceNode, stru.Label, 0, 0); err != nil {
			return nil, err
		}

		// mount
		mountPoint := runMntFor(stru.Label)
		if err := os.MkdirAll(mountPoint, 0755); err != nil {
			return nil, err
		}
		// XXX: is there a better way?
		if output, err := exec.Command("mount", part.KernelDeviceNode, mountPoint).CombinedOutput(); err != nil {
			return nil, osutil.OutputErr(output, err)
		}
		mountPoints = append(mountPoints, mountPoint)
	}

	return mountPoints, nil
}

func unmountFilesystems(mntPts []string) error {
	for _, mntPt := range mntPts {
		if output, err := exec.Command("umount", mntPt).CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	}
	return nil
}

func createClassicRootfsIfNeeded(rootfsCreator string) error {
	dst := runMntFor("ubuntu-data")

	if output, err := exec.Command(rootfsCreator, dst).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}

func createSeedOnTarget(bootDevice, seedLabel string) error {
	// XXX: too naive?
	dataMnt := runMntFor("ubuntu-data")
	src := dirs.SnapSeedDir
	dst := dirs.SnapSeedDirUnder(dataMnt)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if output, err := exec.Command("cp", "-a", src, dst).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}

func run(seedLabel, rootfsCreator, bootDevice string) error {
	logger.Noticef("installing on %q", bootDevice)

	cli := client.New(nil)
	details, err := cli.SystemDetails(seedLabel)
	if err != nil {
		return err
	}
	// TODO: grow the data-partition based on disk size
	laidoutStructs, err := createPartitions(bootDevice, details.Volumes)
	if err != nil {
		return fmt.Errorf("cannot setup partitions: %v", err)
	}
	fmt.Println("laidoutStructs len:", len(laidoutStructs))
	if err := postSystemsInstallSetupStorageEncryption(details); err != nil {
		return fmt.Errorf("cannot setup storage encryption: %v", err)
	}
	mntPts, err := createAndMountFilesystems(bootDevice, details.Volumes)
	if err != nil {
		return fmt.Errorf("cannot create filesystems: %v", err)
	}
	if err := createClassicRootfsIfNeeded(rootfsCreator); err != nil {
		return fmt.Errorf("cannot create classic rootfs: %v", err)
	}
	if err := createSeedOnTarget(bootDevice, seedLabel); err != nil {
		return fmt.Errorf("cannot create seed on target: %v", err)
	}
	// Unmount filesystems
	if err := unmountFilesystems(mntPts); err != nil {
		return fmt.Errorf("cannot unmount filesystems: %v", err)
	}
	if err := postSystemsInstallFinish(cli, details, bootDevice, laidoutStructs); err != nil {
		return fmt.Errorf("cannot finalize install: %v", err)
	}
	// TODO: reboot here automatically (optional)

	return nil
}

func waitForDevice() string {
	for {
		devices, err := emptyFixedBlockDevices()
		if err != nil {
			logger.Noticef("cannot list devices: %v", err)
		}
		switch len(devices) {
		case 0:
			logger.Noticef("cannot use automatic mode, no empty disk found")
		case 1:
			// found exactly one target
			return devices[0]
		default:
			logger.Noticef("cannot use automatic mode, multiple empty disks found: %v", devices)
		}
		time.Sleep(5 * time.Second)
	}
}

func main() {
	if len(os.Args) != 4 {
		// xxx: allow installing real UC without a classic-rootfs later
		fmt.Fprintf(os.Stderr, "need seed-label, target-device and classic-rootfs as argument\n")
		os.Exit(1)
	}
	os.Setenv("SNAPD_DEBUG", "1")
	logger.SimpleSetup()

	seedLabel := os.Args[1]
	rootfsCreator := os.Args[2]
	bootDevice := os.Args[3]

	// this will wait forever for a suitable fixed
	if bootDevice == "auto" {
		bootDevice = waitForDevice()
	}

	if err := run(seedLabel, rootfsCreator, bootDevice); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
	msg := "install done, please remove installation media and reboot"
	fmt.Println(msg)
	exec.Command("wall", msg).Run()
}
