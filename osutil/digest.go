// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"crypto"
	"hash"
	"io"
	"os"

	"github.com/anonymouse64/crypto/sha3"
	"github.com/snapcore/snapd/logger"
)

const (
	hashDigestBufSize = 2 * 1024 * 1024
)

var numSHA3l384 int
var numSHA3l512 int

// FileDigest computes a hash digest of the file using the given hash.
// It also returns the file size.
func FileDigest(filename string, hashType crypto.Hash) ([]byte, uint64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var h hash.Hash
	switch hashType {
	case crypto.SHA3_384:
		numSHA3l384++
		logger.Noticef("SHA3 384 hash %d", numSHA3l384)
		h = sha3.New384()
	case crypto.SHA3_512:
		numSHA3l512++
		logger.Noticef("SHA3 512 hash %d", numSHA3l512)
		h = sha3.New512()
	default:
		h = hashType.New()
	}

	size, err := io.CopyBuffer(h, f, make([]byte, hashDigestBufSize))
	if err != nil {
		return nil, 0, err
	}
	return h.Sum(nil), uint64(size), nil
}
