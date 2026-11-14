// Package experiments runs only hash-confirmed plans, preserving independent evidence.
package experiments

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

type Spec struct {
	RunID     string      `json:"run_id"`
	Directory string      `json:"directory"`
	Plan      domain.Plan `json:"plan"`
	Hash      string      `json:"hash"`
}
type Status struct {
	Protocol          int    `json:"protocol,omitempty"`
	Active            bool   `json:"active,omitempty"`
	RunID             string `json:"run_id"`
	PID               int    `json:"pid"`
	Status            string `json:"status"`
	ExitCode          int    `json:"exit_code"`
	Error             string `json:"error,omitempty"`
	OriginalCommit    string `json:"original_commit"`
	FinalCommit       string `json:"final_commit"`
	Branch            string `json:"branch"`
	Started           string `json:"started_at"`
	Finished          string `json:"finished_at"`
	CompletedCommands int    `json:"completed_commands,omitempty"`
}

type checkpoint struct {
	PlanHash  string `json:"plan_hash"`
	Completed int    `json:"completed_commands"`
	Running   int    `json:"running_command,omitempty"`
	At        string `json:"at"`
}

func Hash(p domain.Plan) string {
	p.Confirmed = false
	p.ConfirmedHash = ""
	sum := sha256.Sum256(domain.Encode(p))
	return hex.EncodeToString(sum[:])
}
func Under(root, path string) bool {
	a, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	b, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func Validate(p domain.Plan) []domain.Issue {
	out := []domain.Issue{}
	missing := func(value, code string) {
		if strings.TrimSpace(value) == "" {
			out = append(out, domain.Issue{Code: code})
		}
	}
	missing(p.Target, "target_required")
	missing(p.Citation, "target_citation_required")
	missing(p.Host.Workdir, "workdir_required")
	missing(p.Host.OS, "os_required")
	missing(p.Host.CPU, "cpu_required")
	missing(p.Host.AllowedRoot, "operation_scope_required")
	missing(p.Repo, "repo_required")
	missing(p.Commit, "commit_required")
	missing(p.Metric, "metric_required")
	missing(p.OutputCSV, "output_csv_required")
	if p.Expected == nil {
		out = append(out, domain.Issue{Code: "expected_value_required"})
	}
	if p.Tolerance < 0 {
		out = append(out, domain.Issue{Code: "invalid_tolerance"})
	}
	if len(p.Commands) == 0 {
		out = append(out, domain.Issue{Code: "commands_required"})
	}
	for _, argv := range p.Commands {
		if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
			out = append(out, domain.Issue{Code: "invalid_command"})
		}
	}
	if len(p.ResumeCommands) > len(p.Commands) {
		out = append(out, domain.Issue{Code: "invalid_resume_commands"})
	}
	for _, argv := range p.ResumeCommands {
		if len(argv) != 0 && strings.TrimSpace(argv[0]) == "" {
			out = append(out, domain.Issue{Code: "invalid_resume_commands"})
			break
		}
	}
	if p.Host.Mode != "manual" && p.Host.Mode != "ssh" && p.Host.Mode != "container" && p.Host.Mode != "local" {
		out = append(out, domain.Issue{Code: "invalid_host_mode"})
	}
	if p.Host.Mode == "ssh" && p.Host.SSH == "" {
		out = append(out, domain.Issue{Code: "ssh_alias_required"})
	}
	if !filepath.IsAbs(p.Host.Workdir) || !filepath.IsAbs(p.Host.AllowedRoot) || !Under(p.Host.AllowedRoot, p.Host.Workdir) {
		out = append(out, domain.Issue{Code: "workdir_outside_scope"})
	}
	if filepath.IsAbs(p.OutputCSV) || !Under("/repo", filepath.Join("/repo", p.OutputCSV)) {
		out = append(out, domain.Issue{Code: "result_path_outside_repo"})
	}
	if len(p.Estimates) == 0 {
		out = append(out, domain.Issue{Code: "estimate_required"})
	}
	return out
}
func UnknownEstimates() []domain.Estimate {
	out := []domain.Estimate{}
	for _, phase := range []string{"download", "build", "preprocess", "train_infer", "validate"} {
		out = append(out, domain.Estimate{Phase: phase, Machine: "unknown", Human: "unknown", Basis: "No reliable measurement for this environment", Confidence: "unknown"})
	}
	return out
}

// Execute is a separate process entrypoint. It can outlive the web service.
func Execute(specFile string) (exit int) {
	b, err := os.ReadFile(specFile)
	if err != nil {
		return 1
	}
	var spec Spec
	if json.Unmarshal(b, &spec) != nil {
		return 1
	}
	if store.ValidateID(spec.RunID) != nil || spec.Hash != Hash(spec.Plan) || spec.Hash != spec.Plan.ConfirmedHash || !spec.Plan.Confirmed || len(Validate(spec.Plan)) > 0 {
		return 1
	}
	if !Under(spec.Plan.Host.AllowedRoot, spec.Directory) ||
		filepath.Clean(spec.Directory) != filepath.Join(spec.Plan.Host.Workdir, "eduforge-"+spec.RunID) {
		return 1
	}
	root, err := filepath.EvalSymlinks(spec.Plan.Host.AllowedRoot)
	if err != nil {
		return 1
	}
	directory, err := filepath.EvalSymlinks(spec.Directory)
	if err != nil || !Under(root, directory) {
		return 1
	}
	spec.Directory = directory
	lock, err := lockRun(spec.Directory)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return 2
	}
	if err != nil {
		return 1
	}
	defer lock.Close()
	status, err := WaitForStatus(spec.Directory)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if _, err := os.Stat(filepath.Join(spec.Directory, "claimed")); err == nil {
			return 2
		}
		status = Status{Protocol: 1, RunID: spec.RunID, Started: domain.Now(), Branch: "eduforge/" + spec.RunID}
	case err != nil || status.RunID != spec.RunID || status.Protocol != 1:
		return 1
	case status.Status == "completed" || status.Status == "failed":
		return status.ExitCode
	}
	status.PID = os.Getpid()
	status.Status = "running"
	status.ExitCode = -1
	save := func() error {
		return store.Atomic(filepath.Join(spec.Directory, "status.json"), domain.Encode(status), 0600)
	}
	if save() != nil {
		return 1
	}
	defer func() {
		status.Finished = domain.Now()
		if status.Status != "completed" {
			status.Status = "failed"
			if status.ExitCode < 0 {
				status.ExitCode = 1
			}
		}
		_ = save()
	}()
	log, err := os.OpenFile(filepath.Join(spec.Directory, "execution.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		status.Error = "log_create_failed"
		return 1
	}
	defer log.Close()
	run := func(dir, name string, args ...string) (string, error) {
		fmt.Fprintf(log, "\n$ %s\n", string(domain.Encode(append([]string{name}, args...))))
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = cleanEnv()
		cmd.ExtraFiles = []*os.File{lock}
		var out bytes.Buffer
		cmd.Stdout = io.MultiWriter(log, &out)
		cmd.Stderr = log
		err := cmd.Run()
		_ = log.Sync()
		return strings.TrimSpace(out.String()), err
	}
	repo := filepath.Join(spec.Directory, "repo")
	if err = AwaitDispatch(spec.Directory, func(value string) {
		if status.Status != value {
			status.Status = value
			_ = save()
		}
	}); err != nil {
		status.Error = err.Error()
		return 1
	}
	if status.OriginalCommit == "" {
		// Publish the checkout only after Git has prepared the pinned branch.
		if _, err := os.Lstat(repo); errors.Is(err, os.ErrNotExist) {
			staging, err := os.MkdirTemp(spec.Directory, "clone-")
			if err != nil {
				status.Error = "git_clone_directory_failed"
				return 1
			}
			defer os.RemoveAll(staging)
			if _, err = run(spec.Directory, "git", "clone", "--no-hardlinks", "--", spec.Plan.Repo, staging); err != nil {
				status.Error = "git_clone_failed"
				return 1
			}
			if _, err = run(staging, "git", "checkout", "--detach", spec.Plan.Commit); err != nil {
				status.Error = "commit_checkout_failed"
				return 1
			}
			if _, err = run(staging, "git", "checkout", "-b", status.Branch); err != nil {
				status.Error = "branch_create_failed"
				return 1
			}
			if err = os.Rename(staging, repo); err != nil {
				status.Error = "git_checkout_save_failed"
				return 1
			}
		}
		status.OriginalCommit, err = run(repo, "git", "rev-parse", "HEAD")
		branch, branchErr := run(repo, "git", "symbolic-ref", "--short", "HEAD")
		if err != nil || branchErr != nil || branch != status.Branch {
			status.Error = "prepared_checkout_invalid"
			return 1
		}
		if save() != nil {
			status.Error = "status_save_failed"
			return 1
		}
	}
	if err = store.Atomic(filepath.Join(spec.Directory, "environment.json"), domain.Encode(spec.Plan.Host), 0600); err != nil {
		status.Error = "environment_save_failed"
		return 1
	}
	checkpointFile := filepath.Join(spec.Directory, "checkpoint.json")
	progress := checkpoint{PlanHash: spec.Hash}
	if raw, err := os.ReadFile(checkpointFile); err == nil {
		if json.Unmarshal(raw, &progress) != nil || progress.PlanHash != spec.Hash ||
			progress.Completed < 0 || progress.Completed > len(spec.Plan.Commands) ||
			(progress.Running != 0 && (progress.Running != progress.Completed+1 || progress.Running > len(spec.Plan.Commands))) {
			status.Error = "checkpoint_invalid"
			return 1
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		status.Error = "checkpoint_read_failed"
		return 1
	}
	saveCheckpoint := func() error {
		progress.At = domain.Now()
		return store.Atomic(checkpointFile, domain.Encode(progress), 0600)
	}
	status.CompletedCommands = progress.Completed
	for i := progress.Completed; i < len(spec.Plan.Commands); i++ {
		if err = AwaitDispatch(spec.Directory, func(value string) {
			if status.Status != value {
				status.Status = value
				_ = save()
			}
		}); err != nil {
			status.Status = "failed"
			status.Error = err.Error()
			return 1
		}
		argv := spec.Plan.Commands[i]
		if progress.Running == i+1 {
			fmt.Fprintf(log, "\nRecovering unfinished command %d\n", i+1)
			if i < len(spec.Plan.ResumeCommands) && len(spec.Plan.ResumeCommands[i]) != 0 {
				argv = spec.Plan.ResumeCommands[i]
			}
		}
		progress.Running = i + 1
		if err = saveCheckpoint(); err != nil {
			status.Error = "checkpoint_write_failed"
			return 1
		}
		fmt.Fprintf(log, "\n$ %s\n", string(domain.Encode(argv)))
		_ = log.Sync()
		if err = ControlledCommand(spec.Directory, repo, argv, log, lock); err != nil {
			status.Error = fmt.Sprintf("command_%d_failed", i+1)
			if err.Error() == "cancelled" {
				status.Error = "cancelled"
			}
			var e *exec.ExitError
			if errors.As(err, &e) {
				status.ExitCode = e.ExitCode()
			}
			return 1
		}
		progress.Completed = i + 1
		progress.Running = 0
		if err = saveCheckpoint(); err != nil {
			status.Error = "checkpoint_write_failed"
			return 1
		}
		status.CompletedCommands = progress.Completed
		_ = save()
	}
	result := filepath.Join(repo, spec.Plan.OutputCSV)
	resolved, err := filepath.EvalSymlinks(result)
	repoResolved, _ := filepath.EvalSymlinks(repo)
	if err != nil || !Under(repoResolved, resolved) {
		status.Error = "result_missing_or_outside_repo"
		return 1
	}
	data, err := ReadEvidence(resolved, 16<<20)
	if err != nil {
		status.Error = "result_read_failed"
		return 1
	}
	if err = store.Atomic(filepath.Join(spec.Directory, "result.csv"), data, 0600); err != nil {
		status.Error = "result_save_failed"
		return 1
	}
	diff, err := run(repo, "git", "diff", "--binary", status.OriginalCommit, "--", ".", ":(exclude).eduforge-run.json")
	if err != nil || store.Atomic(filepath.Join(spec.Directory, "changes.patch"), []byte(diff), 0600) != nil {
		status.Error = "git_diff_save_failed"
		return 1
	}
	sum := sha256.Sum256(data)
	metadata := map[string]any{"run_id": spec.RunID, "plan_hash": spec.Hash, "original_commit": status.OriginalCommit, "commands": spec.Plan.Commands, "resume_commands": spec.Plan.ResumeCommands, "metric": spec.Plan.Metric, "output_csv": spec.Plan.OutputCSV, "result_sha256": hex.EncodeToString(sum[:])}
	expected := domain.Encode(metadata)
	committed, _ := run(repo, "git", "show", "HEAD:.eduforge-run.json")
	if committed != string(expected) {
		// Record tracked changes and explicit metadata. Datasets and weights remain in the run directory.
		_, stageErr := run(repo, "git", "add", "-u")
		if stageErr != nil || store.Atomic(filepath.Join(repo, ".eduforge-run.json"), expected, 0600) != nil {
			status.Error = "git_evidence_stage_failed"
			return 1
		}
		if _, err = run(repo, "git", "add", "--", ".eduforge-run.json"); err != nil {
			status.Error = "git_evidence_stage_failed"
			return 1
		}
		if _, err = run(repo, "git", "-c", "user.name=EduForge", "-c", "user.email=eduforge@localhost", "commit", "-m", "Record confirmed experiment "+spec.RunID); err != nil {
			status.Error = "git_evidence_commit_failed"
			return 1
		}
	}
	status.FinalCommit, err = run(repo, "git", "rev-parse", "HEAD")
	if err != nil {
		status.Error = "git_final_commit_failed"
		return 1
	}
	status.Status = "completed"
	status.ExitCode = 0
	return 0
}
func cleanEnv() []string {
	out := []string{}
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || strings.Contains(name, "API_KEY") || name == "CODEX_HOME" || name == "CLAUDE_CONFIG_DIR" {
			continue
		}
		out = append(out, entry)
	}
	return out
}
func Launch(binary, specFile string) error {
	cmd := exec.Command(binary, "runner", "--spec", specFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
func Alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }
func lockRun(dir string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(dir, "runner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func Inspect(dir string) (Status, error) {
	status, err := WaitForStatus(dir)
	if err != nil || status.Status == "completed" || status.Status == "failed" {
		return status, err
	}
	if status.Protocol == 0 {
		status.Active = Alive(status.PID)
		return status, nil
	}
	lock, err := lockRun(dir)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		status.Active = true
		return status, nil
	}
	if err != nil {
		return status, err
	}
	defer lock.Close()
	return WaitForStatus(dir)
}

func ReadEvidence(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("invalid_evidence_file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("evidence_too_large")
	}
	return data, err
}

func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func WaitForStatus(dir string) (Status, error) {
	var status Status
	b, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err != nil {
		return status, err
	}
	err = json.Unmarshal(b, &status)
	return status, err
}
func InitialRun(planID, hash string, host domain.Host) domain.Run {
	return domain.Run{PlanID: planID, PlanHash: hash, RunID: domain.ID(), Host: host, Status: "not_executed", Comparison: "pending", Evidence: []string{}, Started: ""}
}
