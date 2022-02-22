// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package bootloader

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// sanity - piboot implements the required interfaces
var (
	_ Bootloader                             = (*piboot)(nil)
	_ ExtractedRecoveryKernelImageBootloader = (*piboot)(nil)
	_ NotScriptableBootloader                = (*piboot)(nil)
	_ RebootBootloader                       = (*piboot)(nil)
)

const (
	pibootCfgFilename = "piboot.conf"
	pibootPartFolder  = "/piboot/ubuntu/"
)

// TODO The ubuntu-seed folder should be eventually passed around when
// creating the bootloader.
// This is in a variable so it can be mocked in tests
var ubuntuSeedDir = "/run/mnt/ubuntu-seed/"

type piboot struct {
	rootdir string
	basedir string
}

func (p *piboot) setDefaults() {
	p.basedir = "/boot/piboot/"
}

func (p *piboot) processBlOpts(blOpts *Options) {
	if blOpts == nil {
		return
	}

	switch {
	case blOpts.Role == RoleRecovery || blOpts.NoSlashBoot:
		if !blOpts.PrepareImageTime {
			p.rootdir = ubuntuSeedDir
		}
		// RoleRecovery or NoSlashBoot imply we use
		// the environment file in /piboot/ubuntu as
		// it exists on the partition directly
		p.basedir = pibootPartFolder
	}
}

// newPiboot creates a new Piboot bootloader object
func newPiboot(rootdir string, blOpts *Options) Bootloader {
	p := &piboot{
		rootdir: rootdir,
	}
	p.setDefaults()
	p.processBlOpts(blOpts)

	logger.Noticef("newPiboot: origroot rootdir basedir %q %q %q",
		rootdir, p.rootdir, p.basedir)
	if blOpts != nil {
		logger.Noticef("newPiboot: Options %t %s %t",
			blOpts.PrepareImageTime, blOpts.Role, blOpts.NoSlashBoot)
	}

	return p
}

func (p *piboot) Name() string {
	return "piboot"
}

func (p *piboot) dir() string {
	if p.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(p.rootdir, p.basedir)
}

func (p *piboot) envFile() string {
	return filepath.Join(p.dir(), pibootCfgFilename)
}

// piboot enabled if env file exists
func (p *piboot) Present() (bool, error) {
	logger.Noticef("Checking for %s", p.envFile())
	return osutil.FileExists(p.envFile()), nil
}

// Variables stored in ubuntu-seed:
//   snapd_recovery_system
//   snapd_recovery_mode
//   snapd_recovery_kernel
// Variables stored in ubuntu-boot:
//   kernel_status
//   snap_kernel
//   snap_try_kernel
//   snapd_extra_cmdline_args
//   snapd_full_cmdline_args
//   recovery_system_status
//   try_recovery_system
func (p *piboot) SetBootVars(values map[string]string) error {
	env, err := ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return err
	}

	logger.Noticef("SetBootVars current vals:")
	env.PrintAll()

	// Set when we change a boot env variable, to know if we need to save the env
	dirtyEnv := false
	// Flag to know if we need to write piboot's config.txt or tryboot.txt
	reconfigBootloader := false
	for k, v := range values {
		// already set to the right value, nothing to do
		logger.Noticef("Setting %s=%s", k, v)
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirtyEnv = true
		// Cases that change the bootloader configuration
		if k == "snapd_recovery_mode" || k == "kernel_status" {
			reconfigBootloader = true
		}
		if k == "snap_try_kernel" && v == "" {
			// Refresh (ok or not) finished, remove tryboot.txt.
			// os_prefix in config.txt will be changed now in
			// loadAndApplyConfig in the ok case. Note that removing
			// it is safe as tryboot.txt is used only when a special
			// volatile boot flag is set, so we always have a valid
			// config.txt that will allow booting.
			trybootPath := filepath.Join(ubuntuSeedDir, "tryboot.txt")
			if err := os.Remove(trybootPath); err != nil {
				logger.Noticef("cannot remove %s: %v", trybootPath, err)
			}
		}
	}
	logger.Noticef("SetBootVars:")
	env.PrintAll()

	if dirtyEnv {
		if err := env.Save(); err != nil {
			return err
		}
	}

	if reconfigBootloader {
		if err := p.loadAndApplyConfig(env); err != nil {
			return err
		}
	}

	return nil
}

func (p *piboot) SetBootVarsFromInitramfs(values map[string]string) error {
	env, err := ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return err
	}

	logger.Noticef("SetBootVarsFromInitramfs current vals:")
	env.PrintAll()

	dirtyEnv := false
	for k, v := range values {
		// already set to the right value, nothing to do
		logger.Noticef("Setting %s=%s", k, v)
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirtyEnv = true
	}

	logger.Noticef("SetBootVarsFromInitramfs:")
	env.PrintAll()

	if dirtyEnv {
		if err := env.Save(); err != nil {
			return err
		}
	}

	return nil
}

func (p *piboot) loadAndApplyConfig(env *ubootenv.Env) error {
	var prefix, cfgDir, dstDir string

	cfgFile := "config.txt"
	if env.Get("snapd_recovery_mode") == "run" {
		kernelSnap := env.Get("snap_kernel")
		kernStat := env.Get("kernel_status")
		if kernStat == "try" {
			// snap_try_kernel will be set when installing a new kernel
			kernelSnap = env.Get("snap_try_kernel")
			cfgFile = "tryboot.txt"
		}
		prefix = filepath.Join(pibootPartFolder, kernelSnap)
		cfgDir = ubuntuSeedDir
		dstDir = filepath.Join(ubuntuSeedDir, prefix)
	} else {
		// install/recovery modes, use recovery kernel
		prefix = filepath.Join("/systems", env.Get("snapd_recovery_system"),
			"kernel")
		cfgDir = p.rootdir
		dstDir = filepath.Join(p.rootdir, prefix)
	}

	logger.Noticef("configure piboot %s with prefix %q, cfgDir %q, dstDir %q",
		cfgFile, prefix, cfgDir, dstDir)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	return p.applyConfig(env, cfgFile, prefix, cfgDir, dstDir)
}

// Writes os_prefix in RPi config.txt or tryboot.txt
func (p *piboot) writeRPiCfgWithOsPrefix(prefix, inFile, outFile string) error {
	buf, err := ioutil.ReadFile(inFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(buf), "\n")

	replaced := false
	newOsPrefix := "os_prefix=" + prefix + "/"
	for i, line := range lines {
		if strings.HasPrefix(line, "os_prefix=") {
			if replaced {
				logger.Noticef("unexpected extra os_prefix line: %q", line)
				lines[i] = "# " + lines[i]
				continue
			}
			lines[i] = newOsPrefix
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, newOsPrefix)
		lines = append(lines, "")
	}

	output := strings.Join(lines, "\n")
	return osutil.AtomicWriteFile(outFile, []byte(output), 0644, 0)
}

func (p *piboot) writeCmdline(env *ubootenv.Env, defaultsFile, outFile string) error {
	buf, err := ioutil.ReadFile(defaultsFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(buf), "\n")
	cmdline := lines[0]

	mode := env.Get("snapd_recovery_mode")
	cmdline += " snapd_recovery_mode=" + mode
	if mode != "run" {
		cmdline += " snapd_recovery_system=" + env.Get("snapd_recovery_system")
	}
	// Signal when we are trying a new kernel
	kernelStatus := env.Get("kernel_status")
	if kernelStatus == "try" {
		cmdline += " kernel_status=trying"
	}
	cmdline += "\n"

	logger.Noticef("writing kernel command line to %s", outFile)

	return osutil.AtomicWriteFile(outFile, []byte(cmdline), 0644, 0)
}

// Configure pi bootloader with a given os_prefix. cfgDir contains the
// config files, and dstDir is where we will place the kernel command
// line.
func (p *piboot) applyConfig(env *ubootenv.Env,
	configFile, prefix, cfgDir, dstDir string) error {

	logger.Noticef("applyConfig")
	cmdlineFile := filepath.Join(dstDir, "cmdline.txt")
	refCmdlineFile := filepath.Join(cfgDir, "cmdline.txt")
	currentConfigFile := filepath.Join(cfgDir, "config.txt")

	if err := p.writeCmdline(env, refCmdlineFile, cmdlineFile); err != nil {
		return err
	}
	if err := p.writeRPiCfgWithOsPrefix(prefix, currentConfigFile,
		filepath.Join(cfgDir, configFile)); err != nil {
		return err
	}

	return nil
}

func (p *piboot) GetBootVars(names ...string) (map[string]string, error) {
	logger.Noticef("GetBootVars:")
	env, err := ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = env.Get(name)
		logger.Noticef("%s=%s", name, out[name])
	}

	return out, nil
}

func (p *piboot) InstallBootConfig(gadgetDir string, blOpts *Options) error {
	logger.Noticef("InstallBootConfig: rootdir basedir %q %q", p.rootdir, p.basedir)
	if blOpts != nil {
		logger.Noticef("InstallBootConfig: Options %t %s %t",
			blOpts.PrepareImageTime, blOpts.Role, blOpts.NoSlashBoot)
	}

	// We create an empty env file
	err := os.MkdirAll(filepath.Dir(p.envFile()), 0755)
	if err != nil {
		return err
	}

	// TODO: what's a reasonable size for this file?
	env, err := ubootenv.Create(p.envFile(), 4096)
	if err != nil {
		return err
	}

	return env.Save()
}

func (p *piboot) layoutKernelAssetsToDir(snapf snap.Container, dstDir string) error {
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	if err := extractKernelAssetsToBootDir(dstDir, snapf, assets); err != nil {
		logger.Noticef("layoutKernelAssetsToDir cannot extract files")
		return err
	}

	bcomFiles := filepath.Join(dstDir, "dtbs/broadcom/*")
	if output, err := exec.Command("sh", "-c",
		"mv "+bcomFiles+" "+dstDir).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot move RPi dtbs to %s:\n%s",
			dstDir, output)
	}
	overlaysDir := filepath.Join(dstDir, "dtbs/overlays/")
	newOvDir := filepath.Join(dstDir, "overlays/")
	if err := os.Rename(overlaysDir, newOvDir); err != nil {
		logger.Noticef("layoutKernelAssetsToDir 3")
		if !os.IsExist(err) {
			return err
		}
	}

	// README file is needed so os_prefix is honored for overlays. See
	// https://www.raspberrypi.com/documentation/computers/config_txt.html#os_prefix
	readmeOverlays, err := os.Create(filepath.Join(dstDir, "overlays", "README"))
	if err != nil {
		logger.Noticef("readmeOverlays %s", err)
		return err
	}
	readmeOverlays.Close()
	return nil
}

func (p *piboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	// Rootdir will point to ubuntu-boot, but we need to put things in ubuntu-seed
	dstDir := filepath.Join(ubuntuSeedDir, pibootPartFolder, s.Filename())

	logger.Noticef("ExtractKernelAssets to %s (rootdir %s)", dstDir, p.rootdir)

	return p.layoutKernelAssetsToDir(snapf, dstDir)
}

func (p *piboot) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo,
	snapf snap.Container) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}

	recoveryKernelAssetsDir :=
		filepath.Join(p.rootdir, recoverySystemDir, "kernel")
	logger.Noticef("ExtractRecoveryKernelAssets to %s (%s)",
		recoveryKernelAssetsDir, recoverySystemDir)

	return p.layoutKernelAssetsToDir(snapf, recoveryKernelAssetsDir)
}

func (p *piboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	logger.Noticef("RemoveKernelAssets")
	return removeKernelAssetsFromBootDir(
		filepath.Join(ubuntuSeedDir, pibootPartFolder), s)
}

func (p *piboot) GetRebootArguments() (string, error) {
	env, err := ubootenv.OpenWithFlags(p.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return "", err
	}

	kernStat := env.Get("kernel_status")
	if kernStat == "try" {
		// The reboot parameter makes sure we use tryboot.cfg config
		return "0 tryboot", nil
	}

	return "", nil
}
