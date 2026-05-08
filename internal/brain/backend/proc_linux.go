//go:build linux

package backend

import (
	"os/exec"
	"syscall"
)

// setReapOnParentDeath installs Linux Pdeathsig + Setpgid on the child.
// When the parent (sloptools brain night) exits for any reason, the
// kernel sends SIGTERM to the immediate child. Setpgid puts the child
// in its own process group so the parent can also kill the entire
// group explicitly during graceful shutdown. flock(1) and the language
// CLI wrappers (claude, codex, opencode) all forward SIGTERM to their
// own descendants, so the cascade reaches helpy and sloptools mcp-server
// children as well.
func setReapOnParentDeath(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
