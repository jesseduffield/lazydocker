// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

type registryAuth interface {
	isEmpty() bool
	headerKey() string
}
