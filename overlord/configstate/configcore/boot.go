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

package configcore

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

const (
	OptionBootCmdlineExtra          = "core.system.boot.cmdline-extra"
	OptionBootDangerousCmdlineExtra = "core.system.boot.dangerous-cmdline-extra"
)

func init() {
	supportedConfigurations[OptionBootCmdlineExtra] = true
	supportedConfigurations[OptionBootDangerousCmdlineExtra] = true
}

func changedBootConfigs(c config.Conf) []string {
	changed := []string{}
	for _, name := range c.Changes() {
		if strings.HasPrefix(name, "core.system.boot.") {
			changed = append(changed, name)
		}
	}
	return changed
}

func validateCmdlineExtra(c config.Conf) error {
	for _, opt := range changedBootConfigs(c) {
		optWithoutSnap := strings.SplitN(opt, ".", 2)[1]
		cmdExtra, err := coreCfg(c, optWithoutSnap)
		if err != nil {
			return err
		}

		// TODO check against allowed values from gadget too
		logger.Debugf("validating %s=%q", opt, cmdExtra)
		_, err = osutil.KernelCommandLineSplit(cmdExtra)
		if err != nil {
			return err
		}
	}

	return nil
}

func handleCmdlineExtra(c config.Conf, opts *fsOnlyContext) error {
	bootOpts := changedBootConfigs(c)
	if len(bootOpts) == 0 {
		return nil
	}
	logger.Debugf("handling %v", bootOpts)

	st := c.State()
	st.Lock()
	defer st.Unlock()

	// Create change to set the new kernel command line
	summary := fmt.Sprintf(i18n.G("Appending parameters to kernel command line"))
	chg := st.NewChange("update-cmdline-extra", summary)
	t := st.NewTask("update-gadget-cmdline",
		"Updating command line due to change in system configuration")
	t.Set("is-dynamic", true)
	chg.AddTask(t)
	st.EnsureBefore(0)

	return nil
}
