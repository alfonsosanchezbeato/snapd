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

package configcore

import (
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

// Conf is an interface describing both state and transaction.
type Conf interface {
	Get(snapName, key string, result interface{}) error
	GetMaybe(snapName, key string, result interface{}) error
	GetPristine(snapName, key string, result interface{}) error
	Task() *state.Task
	Set(snapName, key string, value interface{}) error
	Changes() []string
	State() *state.State
}

// ConfGetter is an interface for reading of core config values.
type ConfGetter interface {
	Get(snapName, key string, result interface{}) error
	GetMaybe(snapName, key string, result interface{}) error
	GetPristine(snapName, key string, result interface{}) error
}

// RunTransaction holds a transaction with a task that is in charge of
// appliying a change to the configuration. It is used in the context of
// configcore.
type RunTransaction struct {
	*config.Transaction
	task *state.Task
}

func (rt *RunTransaction) Task() *state.Task {
	return rt.task
}

func NewRunTransaction(tr *config.Transaction, tk *state.Task) *RunTransaction {
	runTransaction := &RunTransaction{Transaction: tr, task: tk}
	return runTransaction
}
