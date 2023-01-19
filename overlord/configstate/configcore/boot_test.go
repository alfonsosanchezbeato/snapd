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

package configcore_test

import (
	. "gopkg.in/check.v1"
)

type bootSuite struct {
	configcoreSuite
}

var _ = Suite(&bootSuite{})

func (s *bootSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
}

// Check that change is created
// func (s *tmpfsSuite) TestConfigureBootCmdlineGoodVals(c *C) {
// 	for _, cmdline := range []string{"param", "par=val"} {
// 		err := configcore.Run(coreDev, &mockConf{
// 			state: s.state,
// 			conf: map[string]interface{}{
// 				configcore.OptionBootCmdlineExtra: cmdline,
// 			},
// 		})
// 		c.Assert(err, IsNil)
// 	}
// }
