//go:build unix

package engine

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// streamCmd runs name and sends each stdout and stderr line to onLine.
// The child is placed in its own process group so cancel also stops
// grandchildren kind launches, such as docker.
func streamCmd(ctx context.Context, env []string, dir, name string, args []string, onLine func(string)) error {
	return streamSplit(ctx, env, dir, name, args, onLine, onLine)
}

// streamSplit is streamCmd with separate stdout and stderr callbacks.
func streamSplit(ctx context.Context, env []string, dir, name string, args []string, onOut, onErr func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	cmd.Env = env
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid

	// exited is set before Wait returns so a late cancel cannot signal a reused pid.
	// lineMu serializes onLine because stdout and stderr are scanned together.
	// It is separate from stateMu so a slow callback cannot block the killer.
	var stateMu, lineMu sync.Mutex
	exited := false
	done := make(chan struct{})
	kill := func(sig syscall.Signal) {
		stateMu.Lock()
		defer stateMu.Unlock()
		if exited {
			return
		}
		_ = syscall.Kill(-pid, sig)
	}
	go func() {
		select {
		case <-ctx.Done():
			kill(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(400 * time.Millisecond):
				kill(syscall.SIGKILL)
			}
		case <-done:
		}
	}()

	var wg sync.WaitGroup
	scan := func(r io.Reader, onLine func(string)) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		for sc.Scan() {
			if onLine == nil {
				continue
			}
			lineMu.Lock()
			onLine(sc.Text())
			lineMu.Unlock()
		}
	}
	wg.Add(2)
	go scan(stdout, onOut)
	go scan(stderr, onErr)
	wg.Wait()
	err = cmd.Wait()
	stateMu.Lock()
	exited = true
	stateMu.Unlock()
	close(done)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
