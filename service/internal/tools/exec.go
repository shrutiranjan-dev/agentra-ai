package tools

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"time"
)

func RunShell(ctx context.Context, cwd, command string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(timeoutCtx, "powershell", "-Command", command)
	} else {
		cmd = exec.CommandContext(timeoutCtx, "sh", "-lc", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if stderr.Len() > 0 {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.Write(stderr.Bytes())
	}
	return out.String(), err
}
