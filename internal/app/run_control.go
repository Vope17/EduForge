package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"eduforge/internal/domain"
	"eduforge/internal/experiments"
	"eduforge/internal/store"
)

func (a *App) ControlRun(ctx context.Context, owner, id, action string) (domain.Record, error) {
	r, run, err := get[domain.Run](ctx, a, owner, id, "run")
	if err != nil {
		return r, err
	}
	if run.Progress == "finished" || run.Progress == "manual" {
		return r, errors.New("run_is_terminal")
	}
	if run.Progress == "needs_environment" {
		return r, errors.New("environment_reconnection_required_after_import")
	}
	if action != "pause" && action != "resume" && action != "cancel" {
		return r, errors.New("invalid_run_action")
	}
	if run.Progress == "queued" || (run.Progress == "paused" && run.Started == "") {
		next := map[string]string{"pause": "paused", "resume": "queued", "cancel": "finished"}[action]
		return set[domain.Run](ctx, a, owner, id, r.Version, func(v *domain.Run) error {
			v.Progress = next
			if action == "cancel" {
				v.Reason = "cancelled_before_execution"
			}
			return nil
		})
	}
	data := domain.Encode(experiments.Control{Action: action})
	file := filepath.Join(a.runDir(run), "control.json")
	if run.Host.Mode == "ssh" {
		_, err = a.ssh(ctx, owner, run.Host.SSH, "cat > "+experiments.ShellQuote(file), data)
	} else {
		if _, err = os.Stat(filepath.Dir(file)); err == nil {
			err = store.Atomic(file, data, 0600)
		}
	}
	if err != nil {
		return r, err
	}
	return set[domain.Run](ctx, a, owner, id, 0, func(v *domain.Run) error {
		switch action {
		case "pause":
			v.Progress = "pause_requested"
			v.Reason = "waiting_for_command_boundary"
		case "resume":
			v.Progress = "running"
			v.Reason = ""
		case "cancel":
			v.Progress = "cancel_requested"
		}
		return nil
	})
}
