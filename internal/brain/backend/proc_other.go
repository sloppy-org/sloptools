//go:build !linux

package backend

import "os/exec"

// setReapOnParentDeath is a no-op on non-Linux platforms. The Pdeathsig
// field is Linux-specific. macOS and Windows users who run brain night
// must accept the orphan-child risk on hard parent termination.
func setReapOnParentDeath(_ *exec.Cmd) {}
