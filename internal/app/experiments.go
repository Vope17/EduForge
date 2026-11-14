package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"eduforge/internal/domain"
	"eduforge/internal/experiments"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,100}$`)

func (a *App) SavePlan(ctx context.Context, owner, paperID string, p domain.Plan) (domain.Record, error) {
	paper, pr, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
	if err != nil {
		return paper, err
	}
	if p.Previous != "" {
		previous, err := a.Store.Get(ctx, owner, p.Previous, "plan")
		if err != nil || previous.Paper != paperID {
			return paper, domain.ErrNotFound
		}
	}
	if pr.KnowledgeID == "" {
		return paper, errors.New("knowledge_required")
	}
	_, k, err := get[domain.Knowledge](ctx, a, owner, pr.KnowledgeID, "knowledge")
	if err != nil {
		return paper, err
	}
	found := false
	for _, u := range k.Units {
		if u.ID == p.Citation {
			found = true
		}
	}
	if !found {
		return paper, errors.New("target_source_citation_required")
	}
	p.Confirmed = false
	p.ConfirmedHash = ""
	if p.Host.Concurrency <= 0 {
		p.Host.Concurrency = 1
	}
	if len(p.Estimates) == 0 {
		p.Estimates = experiments.UnknownEstimates()
	}
	p.Compatibility = experiments.Validate(p)
	p.Missing = []string{}
	for _, issue := range p.Compatibility {
		p.Missing = append(p.Missing, issue.Code)
	}
	return a.create(ctx, owner, "plan", paperID, paper.Project, paperID, p)
}
func (a *App) ConfirmPlan(ctx context.Context, owner, id, hash string) (domain.Record, error) {
	r, p, err := get[domain.Plan](ctx, a, owner, id, "plan")
	if err != nil {
		return r, err
	}
	if hash != experiments.Hash(p) {
		return r, domain.ErrConflict
	}
	issues := experiments.Validate(p)
	if len(issues) > 0 {
		return r, errors.New("plan_incomplete")
	}
	if p.Host.Mode == "ssh" && !aliasPattern.MatchString(p.Host.SSH) {
		return r, errors.New("invalid_ssh_alias")
	}
	if p.Host.Mode == "local" && os.Getenv("EDUFORGE_CONTAINER") == "1" {
		return r, errors.New("choose_container_or_ssh_experiment_host")
	}
	return set[domain.Plan](ctx, a, owner, id, r.Version, func(v *domain.Plan) error {
		v.Confirmed = true
		v.ConfirmedHash = experiments.Hash(*v)
		return nil
	})
}
func (a *App) sshArgs(owner, alias, command string) []string {
	config := filepath.Join(a.Secrets, "ssh", owner, "config")
	known := filepath.Join(a.Secrets, "ssh", owner, "known_hosts")
	return []string{"-F", config, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + known, "-o", "ConnectTimeout=10", alias, command}
}
func (a *App) ssh(ctx context.Context, owner, alias, command string, input []byte) ([]byte, error) {
	if !aliasPattern.MatchString(alias) {
		return nil, errors.New("invalid_ssh_alias")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", a.sshArgs(owner, alias, command)...)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("ssh_connection_or_command_failed")
	}
	return out, nil
}
func (a *App) ProbeHost(ctx context.Context, owner string, h domain.Host) (map[string]any, error) {
	if h.Mode == "manual" {
		return map[string]any{"mode": "manual", "issues": []string{"environment_reported_by_user"}}, nil
	}
	if h.Mode == "ssh" {
		out, err := a.ssh(ctx, owner, h.SSH, "uname -s; uname -m; command -v git; command -v conda || true; command -v docker || true", nil)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		compatible := len(lines) >= 2 && strings.EqualFold(lines[0], runtime.GOOS) && (lines[1] == runtime.GOARCH || (lines[1] == "x86_64" && runtime.GOARCH == "amd64") || (lines[1] == "aarch64" && runtime.GOARCH == "arm64"))
		return map[string]any{"mode": "ssh", "observed": lines, "runner_compatible": compatible, "note": "CPU/GPU/memory and dependencies still require plan verification"}, nil
	}
	_, gitErr := exec.LookPath("git")
	return map[string]any{"mode": h.Mode, "os": runtime.GOOS, "arch": runtime.GOARCH, "git_available": gitErr == nil, "container": os.Getenv("EDUFORGE_CONTAINER") == "1", "note": "This is the service execution environment, not an implicit host connection"}, nil
}
func (a *App) StartRun(ctx context.Context, owner, planID string) (domain.Record, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, p, err := get[domain.Plan](ctx, a, owner, planID, "plan")
	if err != nil {
		return r, err
	}
	if !p.Confirmed || p.ConfirmedHash != experiments.Hash(p) {
		return r, errors.New("confirm_plan_first")
	}
	if len(experiments.Validate(p)) > 0 {
		return r, errors.New("plan_incomplete")
	}
	if p.Host.Mode == "manual" {
		return r, errors.New("upload_manual_evidence_instead")
	}
	runs, err := a.Store.List(ctx, owner, store.Filter{Kind: "run", Paper: r.Paper})
	if err != nil {
		return r, err
	}
	for _, item := range runs {
		run, _ := domain.Decode[domain.Run](item.Data)
		if run.PlanID == planID && run.Progress != "finished" && run.Progress != "manual" {
			return item, nil
		}
	}
	run := experiments.InitialRun(planID, p.ConfirmedHash, p.Host)
	run.Progress = "queued"
	created, err := a.create(ctx, owner, "run", planID, r.Project, r.Paper, run)
	if err != nil {
		return created, err
	}
	return created, nil
}
func (a *App) runDir(run domain.Run) string {
	return filepath.Join(run.Host.Workdir, "eduforge-"+run.RunID)
}
func (a *App) dispatchRun(ctx context.Context, owner string, run domain.Run, p domain.Plan, installed bool) error {
	directory := a.runDir(run)
	spec := experiments.Spec{RunID: run.RunID, Directory: directory, Plan: p, Hash: run.PlanHash}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if run.Host.Mode == "ssh" {
		remoteBinary := filepath.Join(directory, "eduforge-runner")
		remoteSpec := filepath.Join(directory, "spec.json")
		launch := "nohup " + experiments.ShellQuote(remoteBinary) + " runner --spec " + experiments.ShellQuote(remoteSpec) + " </dev/null >>" + experiments.ShellQuote(filepath.Join(directory, "runner.log")) + " 2>&1 &"
		if installed {
			_, err := a.ssh(ctx, owner, run.Host.SSH, launch, nil)
			return err
		}
		probe, err := a.ProbeHost(ctx, owner, run.Host)
		if err != nil {
			return err
		}
		if ok, _ := probe["runner_compatible"].(bool); !ok {
			return errors.New("ssh_runner_os_arch_incompatible_use_manual_or_matching_runner")
		}
		if _, err = a.ssh(ctx, owner, run.Host.SSH, "umask 077; mkdir -p -- "+experiments.ShellQuote(directory), nil); err != nil {
			return err
		}
		b, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		if err = a.installRunFile(ctx, owner, run.Host.SSH, remoteBinary, b, "700"); err != nil {
			return err
		}
		if err = a.installRunFile(ctx, owner, run.Host.SSH, remoteSpec, domain.Encode(spec), "600"); err != nil {
			return err
		}
		_, err = a.ssh(ctx, owner, run.Host.SSH, launch, nil)
		return err
	}
	root, err := filepath.EvalSymlinks(p.Host.AllowedRoot)
	if err != nil {
		return errors.New("allowed_root_not_accessible")
	}
	work, err := filepath.EvalSymlinks(p.Host.Workdir)
	if err != nil || !experiments.Under(root, work) {
		return errors.New("workdir_outside_scope")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	specFile := filepath.Join(directory, "spec.json")
	if err = store.Atomic(specFile, domain.Encode(spec), 0600); err != nil {
		return err
	}
	return experiments.Launch(binary, specFile)
}

func (a *App) installRunFile(ctx context.Context, owner, alias, path string, data []byte, mode string) error {
	final := experiments.ShellQuote(path)
	staging := experiments.ShellQuote(path + ".incoming")
	command := fmt.Sprintf("umask 077; if test -f %s; then cat >/dev/null; else cat >%s && test \"$(wc -c <%s)\" -eq %d && chmod %s %s && mv -- %s %s; fi",
		final, staging, staging, len(data), mode, staging, staging, final)
	_, err := a.ssh(ctx, owner, alias, command, data)
	return err
}
func (a *App) pollRuns(ctx context.Context, owner string) {
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: "run"})
	if err != nil {
		return
	}
	active := map[string]int{}
	for _, r := range items {
		run, _ := domain.Decode[domain.Run](r.Data)
		if run.Progress == "running" || run.Progress == "reconciling" || (run.Progress == "paused" && run.Started != "") || run.Progress == "pause_requested" || run.Progress == "cancel_requested" {
			active[hostKey(run.Host)]++
		}
	}
	for _, r := range items {
		run, _ := domain.Decode[domain.Run](r.Data)
		if run.Progress == "finished" || run.Progress == "manual" || (run.Progress == "paused" && run.Started == "") {
			continue
		}
		_, plan, err := get[domain.Plan](ctx, a, owner, run.PlanID, "plan")
		if err != nil {
			continue
		}
		if run.Progress == "queued" {
			limit := max(1, run.Host.Concurrency)
			if active[hostKey(run.Host)] >= limit {
				continue
			}
			if a.launchRun(ctx, owner, r, run, plan, false) {
				active[hostKey(run.Host)]++
			}
			continue
		}
		if run.Progress != "running" && run.Progress != "reconciling" && run.Progress != "paused" && run.Progress != "pause_requested" && run.Progress != "cancel_requested" {
			continue
		}
		status, err := a.readStatus(ctx, owner, run)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && retryDispatch(run) && run.Progress != "paused" {
				a.launchRun(ctx, owner, r, run, plan, run.Started != "")
			} else if !errors.Is(err, os.ErrNotExist) && run.Progress != "reconciling" {
				_, _ = set[domain.Run](ctx, a, owner, r.ID, r.Version, func(v *domain.Run) error {
					v.Progress = "reconciling"
					v.Reason = "reconnect_required"
					return nil
				})
			}
			continue
		}
		if status.Status == "running" || status.Status == "paused" {
			if status.Protocol == 1 && !status.Active && retryDispatch(run) && run.Progress != "paused" {
				a.launchRun(ctx, owner, r, run, plan, true)
				continue
			}
			next, reason := status.Status, ""
			if status.Status == "paused" {
				reason = "paused_at_command_boundary"
			}
			if status.Protocol == 0 && !status.Active && run.Host.Mode != "ssh" {
				next, reason = "reconciling", "legacy_runner_checkpoint_review_required"
			}
			if next == "running" && (run.Progress == "pause_requested" || run.Progress == "cancel_requested") {
				next, reason = run.Progress, run.Reason
			}
			if run.Progress != next || run.Started != status.Started || run.CompletedCommands != status.CompletedCommands || run.Reason != reason {
				_, _ = set[domain.Run](ctx, a, owner, r.ID, r.Version, func(v *domain.Run) error {
					v.Progress, v.Reason = next, reason
					v.Started = status.Started
					v.CompletedCommands = status.CompletedCommands
					return nil
				})
			}
			continue
		}
		if status.Status != "completed" && status.Status != "failed" {
			continue
		}
		evidence := []string{}
		for _, name := range []string{"execution.log", "environment.json", "checkpoint.json", "changes.patch", "result.csv", "status.json"} {
			b, err := a.readRunFile(ctx, owner, run, name)
			if err == nil {
				rel, _, e := a.Store.Blob(b, filepath.Ext(name))
				if e == nil {
					evidence = append(evidence, rel)
				}
			}
		}
		actual := (*float64)(nil)
		comparison, reason := "pending", status.Error
		if status.Status == "completed" {
			b, err := a.readRunFile(ctx, owner, run, "result.csv")
			if err == nil {
				actual, comparison, reason, _ = experiments.CompareCSV(b, plan, plan.Conditions)
			}
			diff, err := a.readRunFile(ctx, owner, run, "changes.patch")
			if err == nil && len(bytes.TrimSpace(diff)) > 0 {
				comparison = "pending"
				reason = "code_changes_require_method_and_evaluation_review"
			}
		}
		_, _ = set[domain.Run](ctx, a, owner, r.ID, 0, func(v *domain.Run) error {
			v.Progress = "finished"
			v.Status = status.Status
			v.Comparison = comparison
			v.Reason = reason
			v.Evidence = evidence
			v.GitCommit = status.FinalCommit
			v.OriginalCommit = status.OriginalCommit
			v.Started = status.Started
			v.CompletedCommands = status.CompletedCommands
			v.Branch = status.Branch
			v.Actual = actual
			v.Finished = status.Finished
			return nil
		})
	}
}

func retryDispatch(run domain.Run) bool {
	at, err := time.Parse(time.RFC3339Nano, run.Dispatched)
	return err != nil || time.Since(at) >= 3*time.Second
}

func (a *App) launchRun(ctx context.Context, owner string, r domain.Record, run domain.Run, plan domain.Plan, installed bool) bool {
	row, err := set[domain.Run](ctx, a, owner, r.ID, r.Version, func(v *domain.Run) error {
		v.Dispatched = domain.Now()
		if v.Progress == "queued" {
			v.Progress = "reconciling"
		}
		return nil
	})
	if err != nil {
		return false
	}
	err = a.dispatchRun(ctx, owner, run, plan, installed)
	if err != nil {
		_, _ = set[domain.Run](ctx, a, owner, row.ID, row.Version, func(v *domain.Run) error {
			v.Reason = err.Error()
			return nil
		})
	}
	return true
}
func hostKey(h domain.Host) string {
	if h.Mode == "ssh" {
		return "ssh:" + h.SSH
	}
	return "service"
}
func (a *App) readStatus(ctx context.Context, owner string, run domain.Run) (experiments.Status, error) {
	var status experiments.Status
	var err error
	directory := a.runDir(run)
	if run.Host.Mode == "ssh" {
		binary := experiments.ShellQuote(filepath.Join(directory, "eduforge-runner"))
		file := experiments.ShellQuote(filepath.Join(directory, "status.json"))
		command := "if test -f " + file + "; then " + binary + " runner-status --dir " + experiments.ShellQuote(directory) + " || cat -- " + file + "; fi"
		raw, e := a.ssh(ctx, owner, run.Host.SSH, command, nil)
		if e != nil {
			return status, e
		}
		if len(raw) == 0 {
			return status, os.ErrNotExist
		}
		err = json.Unmarshal(raw, &status)
	} else {
		status, err = experiments.Inspect(directory)
	}
	if err == nil && status.RunID != run.RunID {
		return status, errors.New("run_identity_mismatch")
	}
	return status, err
}
func (a *App) readRunFile(ctx context.Context, owner string, run domain.Run, name string) ([]byte, error) {
	if filepath.Base(name) != name {
		return nil, errors.New("invalid_evidence_name")
	}
	file := filepath.Join(a.runDir(run), name)
	if run.Host.Mode == "ssh" {
		return a.ssh(ctx, owner, run.Host.SSH, "cat -- "+experiments.ShellQuote(file), nil)
	}
	return experiments.ReadEvidence(file, 32<<20)
}
func (a *App) ManualEvidence(ctx context.Context, owner, planID string, log, csv []byte, conditions domain.Conditions, executionStatus string) (domain.Record, error) {
	r, p, err := get[domain.Plan](ctx, a, owner, planID, "plan")
	if err != nil {
		return r, err
	}
	if !p.Confirmed || p.ConfirmedHash != experiments.Hash(p) {
		return r, errors.New("confirm_plan_before_evidence")
	}
	if len(log) == 0 || len(csv) == 0 {
		return r, errors.New("log_and_csv_required")
	}
	if executionStatus != "completed" && executionStatus != "failed" {
		return r, errors.New("invalid_execution_status")
	}
	run := experiments.InitialRun(planID, p.ConfirmedHash, p.Host)
	run.Progress = "manual"
	run.Status = executionStatus
	run.GitCommit = p.Commit
	run.Finished = domain.Now()
	run.Actual, run.Comparison, run.Reason, err = experiments.CompareCSV(csv, p, conditions)
	if err != nil {
		return r, err
	}
	if executionStatus == "failed" {
		run.Comparison = "pending"
		run.Reason = "execution_failed"
	}
	for _, item := range []struct {
		b   []byte
		ext string
	}{{log, ".log"}, {csv, ".csv"}, {domain.Encode(conditions), ".json"}} {
		rel, _, err := a.Store.Blob(item.b, item.ext)
		if err != nil {
			return r, err
		}
		run.Evidence = append(run.Evidence, rel)
	}
	return a.create(ctx, owner, "run", planID, r.Project, r.Paper, run)
}

// PlanChatReply returns the assistant's next message in a planning chat.
func (a *App) PlanChatReply(ctx context.Context, owner, paperID string, messages []domain.ChatMessage, grill bool, lang string) (string, error) {
	_, p, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
	if err != nil {
		return "", err
	}
	if p.KnowledgeID == "" {
		return "", errors.New("confirm_knowledge_first")
	}
	_, k, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	if err != nil {
		return "", err
	}
	if len(messages) == 0 || len(messages) > 200 {
		return "", errors.New("invalid_chat")
	}
	for _, m := range messages {
		if (m.Role != "user" && m.Role != "assistant") || len(m.Text) > 20000 {
			return "", errors.New("invalid_chat")
		}
	}
	if !domain.Language(lang) {
		lang = "zh-TW"
	}
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return "", err
	}
	return a.Providers.Chat(ctx, owner, refs, research.ChatPrompt(k, lang, grill, messages))
}

// SavePlanChat keeps a paper's planning conversation (one per paper).
func (a *App) SavePlanChat(ctx context.Context, owner, paperID string, chat domain.PlanChat) (domain.Record, error) {
	paper, _, err := get[domain.Paper](ctx, a, owner, paperID, "paper")
	if err != nil {
		return domain.Record{}, err
	}
	if len(chat.Messages) > 400 {
		return domain.Record{}, errors.New("invalid_chat")
	}
	for _, m := range chat.Messages {
		if (m.Role != "user" && m.Role != "assistant") || len(m.Text) > 20000 {
			return domain.Record{}, errors.New("invalid_chat")
		}
	}
	chat.SavedAt = domain.Now()
	existing, err := a.Store.List(ctx, owner, store.Filter{Kind: "plan_chat", Paper: paperID})
	if err != nil {
		return domain.Record{}, err
	}
	if len(existing) > 0 {
		return set[domain.PlanChat](ctx, a, owner, existing[len(existing)-1].ID, 0, func(old *domain.PlanChat) error {
			*old = chat
			return nil
		})
	}
	return a.create(ctx, owner, "plan_chat", paperID, paper.Project, paperID, chat)
}
