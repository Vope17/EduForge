package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/llm"
	"eduforge/backend/internal/store"
	"eduforge/backend/internal/template"
)

var ErrUpstream = errors.New("generation request failed")
var ErrInput = errors.New("invalid generation input")

type Input struct {
	Type       artifact.Type     `json:"artifact_type,omitempty"`
	SlideCount int               `json:"slide_count,omitempty"`
	Topic      string            `json:"topic"`
	Audience   string            `json:"audience"`
	Kind       string            `json:"kind"`
	Language   artifact.Language `json:"language"`
}

type Service struct {
	store    *store.Store
	provider llm.Provider
	limit    int
}

func New(db *store.Store, provider llm.Provider, dailyLimit int) *Service {
	return &Service{store: db, provider: provider, limit: dailyLimit}
}

func (s *Service) Ready() bool { return s.provider != nil && s.provider.Ready() }

func (s *Service) Create(ctx context.Context, user string, input Input) (template.Request, error) {
	if input.Type == "" {
		input.Type = artifact.DocumentType
	}
	input.Topic = strings.TrimSpace(input.Topic)
	input.Audience = strings.TrimSpace(input.Audience)
	if input.Topic == "" || utf8.RuneCountInString(input.Topic) > 160 ||
		utf8.RuneCountInString(input.Audience) > 100 || !input.Language.Valid() {
		return template.Request{}, ErrInput
	}
	if !input.Type.Valid() {
		return template.Request{}, ErrInput
	}
	if input.Type == artifact.SlidesType {
		if input.Kind != "presentation" || input.SlideCount < 1 || input.SlideCount > artifact.MaxSlides {
			return template.Request{}, ErrInput
		}
	} else {
		if input.SlideCount != 0 {
			return template.Request{}, ErrInput
		}
		switch input.Kind {
		case "lesson_plan", "worksheet", "handout":
		default:
			return template.Request{}, ErrInput
		}
	}
	r := template.Request{
		Type: input.Type, SlideCount: input.SlideCount, ID: artifact.NewID(), State: template.Requested, Language: input.Language,
		Topic: input.Topic, Audience: input.Audience, Kind: input.Kind,
	}
	return r, s.store.CreateRequest(ctx, user, r)
}

func (s *Service) Preview(ctx context.Context, user, id string, version int, feedback string) (_ template.Request, err error) {
	feedback = strings.TrimSpace(feedback)
	if utf8.RuneCountInString(feedback) > 400 {
		return template.Request{}, ErrInput
	}
	r, err := s.store.Request(ctx, user, id)
	if err != nil {
		return r, err
	}
	if r.Version != version || r.State != template.Requested && r.State != template.AwaitingConfirmation {
		return r, store.ErrConflict
	}
	if r.State == template.AwaitingConfirmation && feedback == "" {
		return r, ErrInput
	}
	if s.provider == nil || !s.provider.Ready() {
		return r, llm.ErrUnavailable
	}
	if err := s.store.ClaimPreview(ctx, user, r, s.limit); err != nil {
		return r, err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, s.store.ReleasePreview(cleanup, user, id, r.Version))
		}
	}()

	data := struct {
		Input
		Previous *template.Spec `json:"previous_template,omitempty"`
		Feedback string         `json:"feedback,omitempty"`
	}{inputFrom(r), r.Spec, feedback}
	var output struct {
		Language artifact.Language `json:"language"`
		template.Spec
	}
	instruction, schema, tokens := previewInstruction, previewSchema, 1200
	if r.Type == artifact.SlidesType {
		instruction, schema, tokens = slidePreviewInstruction, slidePreviewSchema, 1600
	}
	err = s.generate(ctx, 25*time.Second, instruction, data, schema, tokens, &output)
	if err != nil {
		return r, err
	}
	if output.Language != r.Language || output.Spec.ValidateFor(r.Type) != nil {
		return r, llm.ErrInvalidOutput
	}
	if err := s.store.PublishPreview(ctx, user, r, output.Spec); err != nil {
		return r, err
	}
	return s.store.Request(ctx, user, id)
}

func (s *Service) Confirm(ctx context.Context, user, id string, version int) (template.Request, error) {
	if err := s.store.Confirm(ctx, user, id, version); err != nil {
		return template.Request{}, err
	}
	return s.store.Request(ctx, user, id)
}

func (s *Service) Generate(ctx context.Context, user, id string, version int) (_ artifact.Artifact, err error) {
	r, err := s.store.Request(ctx, user, id)
	if err != nil {
		return artifact.Artifact{}, err
	}
	if r.State != template.Confirmed || r.Spec == nil || version != r.Version || version != r.ConfirmedVersion {
		return artifact.Artifact{}, store.ErrConflict
	}
	if s.provider == nil || !s.provider.Ready() {
		return artifact.Artifact{}, llm.ErrUnavailable
	}
	if err := s.store.ClaimGeneration(ctx, user, r, s.limit); err != nil {
		return artifact.Artifact{}, err
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, s.store.ReleaseGeneration(cleanup, user, id, version))
		}
	}()

	data := struct {
		Input
		Template template.Spec `json:"confirmed_template"`
	}{inputFrom(r), *r.Spec}
	var output struct {
		Language artifact.Language `json:"language"`
		artifact.Content
	}
	instruction, schema, timeout, tokens := documentInstruction, documentSchema, 55*time.Second, 4500
	if r.Type == artifact.SlidesType {
		instruction, schema, timeout, tokens = slidesInstruction, slidesContentSchema, 180*time.Second, 8000
	}
	err = s.generate(ctx, timeout, instruction, data, schema, tokens, &output)
	if err != nil {
		return artifact.Artifact{}, err
	}
	if output.Language != r.Language || output.Content.ValidateFor(r.Type) != nil {
		return artifact.Artifact{}, llm.ErrInvalidOutput
	}
	if r.Type == artifact.SlidesType {
		if len(output.Blocks) != r.SlideCount {
			return artifact.Artifact{}, llm.ErrInvalidOutput
		}
		for _, page := range output.Blocks {
			if page.Layout != r.Spec.Layout {
				return artifact.Artifact{}, llm.ErrInvalidOutput
			}
		}
	}
	for _, block := range artifact.FlattenBlocks(output.Blocks) {
		if block.ID != "" || block.Kind == "image" && (r.Type != artifact.SlidesType || block.ImageURL != "") {
			return artifact.Artifact{}, llm.ErrInvalidOutput
		}
	}
	if err := output.Content.AssignIDs(nil); err != nil {
		return artifact.Artifact{}, fmt.Errorf("assign generated block IDs: %w", err)
	}
	if err := s.store.CompleteGeneration(ctx, user, r, output.Content); err != nil {
		return artifact.Artifact{}, err
	}
	return s.store.Document(ctx, user, id, 1)
}

func inputFrom(r template.Request) Input {
	return Input{Type: r.Type, SlideCount: r.SlideCount, Topic: r.Topic, Audience: r.Audience, Kind: r.Kind, Language: r.Language}
}

func (s *Service) generate(ctx context.Context, timeout time.Duration, instruction string, data any, schema json.RawMessage, maxTokens int, output any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode generation input: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := s.provider.Generate(callCtx, llm.Request{
		Instruction: instruction, Data: payload, Schema: schema, MaxTokens: maxTokens,
	})
	if err != nil {
		if errors.Is(err, llm.ErrUnavailable) || errors.Is(err, llm.ErrInvalidOutput) ||
			errors.Is(err, llm.ErrPlanPermission) || errors.Is(err, llm.ErrSubscriptionLimit) {
			return err
		}
		return ErrUpstream
	}
	if len(raw) == 0 || len(raw) > 256<<10 {
		return llm.ErrInvalidOutput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return llm.ErrInvalidOutput
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return llm.ErrInvalidOutput
	}
	return nil
}
