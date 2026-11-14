package experiments

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Control struct {
	Action string `json:"action"`
}

func ReadControl(directory string) string {
	b, err := os.ReadFile(filepath.Join(directory, "control.json"))
	if err != nil {
		return ""
	}
	var c Control
	_ = json.Unmarshal(b, &c)
	return c.Action
}
func AwaitDispatch(directory string, save func(string)) error {
	for {
		switch ReadControl(directory) {
		case "cancel":
			return errors.New("cancelled")
		case "pause":
			save("paused")
			time.Sleep(500 * time.Millisecond)
		default:
			save("running")
			return nil
		}
	}
}

// Pause waits for a command boundary. Cancellation terminates only this command's process group.
func ControlledCommand(directory, cwd string, argv []string, log io.Writer, lock *os.File) error {
	if len(argv) == 0 {
		return errors.New("empty_command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = cleanEnv()
	cmd.ExtraFiles = []*os.File{lock}
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if ReadControl(directory) == "cancel" {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
					select {
					case <-done:
						return
					case <-time.After(10 * time.Second):
						_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					}
					return
				}
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if ReadControl(directory) == "cancel" {
		return errors.New("cancelled")
	}
	return err
}
