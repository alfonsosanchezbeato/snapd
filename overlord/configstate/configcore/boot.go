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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func init() {
	supportedConfigurations["core.system.boot.cmdline-extra"] = true
	supportedConfigurations["core.system.boot.dangerous-cmdline-extra"] = true
}

func validateCmdlineExtra(c config.Conf) error {
	// TODO check against allowed values from gadget? Or do that in the task?
	return nil
}

func handleCmdlineExtra(c config.Conf, opts *fsOnlyContext) error {
	logger.Debugf("system.boot.* being handled")

	// TODO Check if the options have changed? Can that be done here?

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
