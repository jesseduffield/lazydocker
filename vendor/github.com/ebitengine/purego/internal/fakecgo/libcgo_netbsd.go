// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Ebitengine Authors

//go:build !cgo

package fakecgo

type (
	pthread_cond_t  uintptr
	pthread_mutex_t uintptr
)

var (
	PTHREAD_COND_INITIALIZER  = pthread_cond_t(0)
	PTHREAD_MUTEX_INITIALIZER = pthread_mutex_t(0)
)

// Source: https://github.com/NetBSD/src/blob/613e27c65223fd2283b6ed679da1197e12f50e27/sys/compat/linux/arch/m68k/linux_signal.h#L133
type stack_t struct {
	ss_sp    uintptr
	ss_flags int32
	ss_size  uintptr
}

// Source: https://github.com/NetBSD/src/blob/613e27c65223fd2283b6ed679da1197e12f50e27/sys/sys/signal.h#L261
const SS_DISABLE = 0x004
