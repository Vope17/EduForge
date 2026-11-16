package app

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// Curriculum order and inheritance.
//
// A course holds chapters, a chapter holds lessons, and a lesson may also be
// unfiled (no chapter). The teaching order is: unfiled lessons first (by
// position), then each chapter in position order with its lessons in position
// order. A lesson's "prior lessons" are all lessons before it in that order;
// their concepts count as already taught.

// nextPosition is one past the highest position among a course's records of
// kind ("chapter" or "lesson"), so a new one goes last.
func (a *App) nextPosition(ctx context.Context, owner, course, kind string) (int, error) {
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: kind, Course: course})
	if err != nil {
		return 0, err
	}
	next := 0
	for _, r := range items {
		position := 0
		switch kind {
		case "chapter":
			v, _ := domain.Decode[domain.Chapter](r.Data)
			position = v.Position
		case "lesson":
			v, _ := domain.Decode[domain.Lesson](r.Data)
			position = v.Position
		}
		next = max(next, position+1)
	}
	return next, nil
}

// LessonGroup lists the lessons that follow each other under one chapter
// ("" for unfiled lessons), as the user arranged them.
type LessonGroup struct {
	Chapter string   `json:"chapter"`
	Lessons []string `json:"lessons"`
}

// CourseOrder is a complete arrangement of a course: the chapters in order and
// every lesson's chapter and place. Lessons absent from it keep their record.
type CourseOrder struct {
	Chapters []string      `json:"chapters"`
	Groups   []LessonGroup `json:"groups"`
}

// ReorderCourse applies a drag-and-drop arrangement. Every id must belong to
// the course, appear once, and the chapters list must name every chapter.
func (a *App) ReorderCourse(ctx context.Context, owner, course string, order CourseOrder) error {
	if _, err := a.Store.Get(ctx, owner, course, "course"); err != nil {
		return err
	}
	chapters, err := a.Store.List(ctx, owner, store.Filter{Kind: "chapter", Course: course})
	if err != nil {
		return err
	}
	lessons, err := a.Store.List(ctx, owner, store.Filter{Kind: "lesson", Course: course})
	if err != nil {
		return err
	}
	chapterIDs := map[string]bool{}
	for _, c := range chapters {
		chapterIDs[c.ID] = true
	}
	lessonIDs := map[string]bool{}
	for _, l := range lessons {
		lessonIDs[l.ID] = true
	}
	if len(order.Chapters) != len(chapterIDs) {
		return errors.New("order_chapters_incomplete")
	}
	seen := map[string]bool{}
	for _, id := range order.Chapters {
		if !chapterIDs[id] || seen[id] {
			return errors.New("order_unknown_chapter")
		}
		seen[id] = true
	}
	seenLesson := map[string]bool{}
	for _, group := range order.Groups {
		if group.Chapter != "" && !chapterIDs[group.Chapter] {
			return errors.New("order_unknown_chapter")
		}
		for _, id := range group.Lessons {
			if !lessonIDs[id] || seenLesson[id] {
				return errors.New("order_unknown_lesson")
			}
			seenLesson[id] = true
		}
	}
	if len(seenLesson) != len(lessonIDs) {
		return errors.New("order_lessons_incomplete")
	}
	for position, id := range order.Chapters {
		if _, err = set[domain.Chapter](ctx, a, owner, id, 0, func(c *domain.Chapter) error {
			c.Position = position
			return nil
		}); err != nil {
			return err
		}
	}
	for _, group := range order.Groups {
		for position, id := range group.Lessons {
			if _, err = set[domain.Lesson](ctx, a, owner, id, 0, func(l *domain.Lesson) error {
				l.Chapter = group.Chapter
				l.Position = position
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// OrderedLessons returns a course's lessons in teaching order.
func (a *App) OrderedLessons(ctx context.Context, owner, course string) ([]domain.Record, error) {
	chapters, err := a.Store.List(ctx, owner, store.Filter{Kind: "chapter", Course: course})
	if err != nil {
		return nil, err
	}
	lessons, err := a.Store.List(ctx, owner, store.Filter{Kind: "lesson", Course: course})
	if err != nil {
		return nil, err
	}
	return teachingOrder(chapters, lessons), nil
}

// teachingOrder sorts lessons: unfiled first, then by chapter position, each
// by lesson position, falling back to creation order (the list order).
func teachingOrder(chapters, lessons []domain.Record) []domain.Record {
	chapterPosition := map[string]int{}
	for _, c := range chapters {
		v, _ := domain.Decode[domain.Chapter](c.Data)
		chapterPosition[c.ID] = v.Position
	}
	type keyed struct {
		record  domain.Record
		chapter int
		lesson  int
	}
	items := make([]keyed, 0, len(lessons))
	for _, r := range lessons {
		v, _ := domain.Decode[domain.Lesson](r.Data)
		chapter := -1
		if position, ok := chapterPosition[v.Chapter]; ok && v.Chapter != "" {
			chapter = position
		}
		items = append(items, keyed{r, chapter, v.Position})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].chapter != items[j].chapter {
			return items[i].chapter < items[j].chapter
		}
		return items[i].lesson < items[j].lesson
	})
	out := make([]domain.Record, len(items))
	for i, item := range items {
		out[i] = item.record
	}
	return out
}

// PriorLessons returns the lessons taught before the given one.
func (a *App) PriorLessons(ctx context.Context, owner, lessonID string) ([]domain.Record, error) {
	lesson, err := a.Store.Get(ctx, owner, lessonID, "lesson")
	if err != nil {
		return nil, err
	}
	ordered, err := a.OrderedLessons(ctx, owner, lesson.Course)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(ordered, func(r domain.Record) bool { return r.ID == lessonID })
	if index < 0 {
		return nil, domain.ErrNotFound
	}
	return ordered[:index], nil
}

// backgroundOf records the lessons now taught before lessonID; nil when the
// lesson cannot be found.
func (a *App) backgroundOf(ctx context.Context, owner, lessonID string) *domain.Background {
	prior, err := a.PriorLessons(ctx, owner, lessonID)
	if err != nil {
		return nil
	}
	ids := make([]string, len(prior))
	for i, r := range prior {
		ids[i] = r.ID
	}
	return &domain.Background{Lessons: ids}
}

// BackgroundChanged lists the ids of a course's current knowledge and outputs
// whose recorded background no longer matches the lessons now taught before
// their lesson (PriorLessons): lessons were reordered, moved between chapters,
// added before it or deleted. Only which lessons came before matters, not
// their order among themselves. Records without a background (rule-based
// knowledge, the syllabus, outputs from before backgrounds were recorded) are
// never listed.
func (a *App) BackgroundChanged(ctx context.Context, owner, course string) ([]string, error) {
	ordered, err := a.OrderedLessons(ctx, owner, course)
	if err != nil {
		return nil, err
	}
	prior := map[string]map[string]bool{}
	current := map[string]bool{} // the knowledge each lesson uses now
	seen := map[string]bool{}
	for _, r := range ordered {
		prior[r.ID] = maps.Clone(seen)
		seen[r.ID] = true
		if l, err := domain.Decode[domain.Lesson](r.Data); err == nil && l.KnowledgeID != "" {
			current[l.KnowledgeID] = true
		}
	}
	changed := func(lesson string, b *domain.Background) bool {
		now, ok := prior[lesson]
		if b == nil || !ok {
			return false
		}
		then := map[string]bool{}
		for _, id := range b.Lessons {
			then[id] = true
		}
		return !maps.Equal(then, now)
	}
	out := []string{}
	knowledge, err := a.Store.List(ctx, owner, store.Filter{Kind: "knowledge", Course: course})
	if err != nil {
		return nil, err
	}
	for _, r := range knowledge {
		if k, err := domain.Decode[domain.Knowledge](r.Data); err == nil && current[r.ID] && changed(r.Lesson, k.Background) {
			out = append(out, r.ID)
		}
	}
	outputs, err := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Course: course})
	if err != nil {
		return nil, err
	}
	for _, r := range outputs {
		if art, err := domain.Decode[domain.Artifact](r.Data); err == nil && changed(r.Lesson, art.Background) {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

// MergeSettings overlays the non-zero fields of over onto base. Zero values
// mean "inherit", so a chapter or lesson stores only what it changes.
func MergeSettings(base, over domain.OutputSettings) domain.OutputSettings {
	out := base
	out.Languages = maps.Clone(base.Languages)
	out.Instructions = maps.Clone(base.Instructions)
	out.Questions = maps.Clone(base.Questions)
	if len(over.Selected) > 0 {
		out.Selected = slices.Clone(over.Selected)
	}
	if over.Depth != "" {
		out.Depth = over.Depth
	}
	if over.Level != "" {
		out.Level = over.Level
	}
	if over.Audience != "" {
		out.Audience = over.Audience
	}
	if over.Minutes != 0 {
		out.Minutes = over.Minutes
	}
	if over.TemplateID != "" {
		out.TemplateID = over.TemplateID
	}
	if over.ReaderTemplateID != "" {
		out.ReaderTemplateID = over.ReaderTemplateID
	}
	if over.PlanFormID != "" {
		out.PlanFormID = over.PlanFormID
	}
	if len(over.Languages) > 0 && out.Languages == nil {
		out.Languages = map[string]string{}
	}
	for kind, lang := range over.Languages {
		if lang != "" {
			out.Languages[kind] = lang
		}
	}
	if len(over.Instructions) > 0 && out.Instructions == nil {
		out.Instructions = map[string]string{}
	}
	for kind, text := range over.Instructions {
		if text != "" {
			out.Instructions[kind] = text
		}
	}
	for kind, n := range over.Questions {
		if n > 0 {
			if out.Questions == nil {
				out.Questions = map[string]int{}
			}
			out.Questions[kind] = n
		}
	}
	return NormalizeSettings(out)
}

// EffectiveSettings resolves a lesson's settings: course defaults, then its
// chapter's overrides, then its own.
func (a *App) EffectiveSettings(ctx context.Context, owner, lessonID string) (domain.OutputSettings, error) {
	r, lesson, err := get[domain.Lesson](ctx, a, owner, lessonID, "lesson")
	if err != nil {
		return domain.OutputSettings{}, err
	}
	_, course, err := get[domain.Course](ctx, a, owner, r.Course, "course")
	if err != nil {
		return domain.OutputSettings{}, err
	}
	settings := NormalizeSettings(course.Settings)
	if lesson.Chapter != "" {
		if _, chapter, e := get[domain.Chapter](ctx, a, owner, lesson.Chapter, "chapter"); e == nil {
			settings = MergeSettings(settings, chapter.Settings)
		}
	}
	return MergeSettings(settings, lesson.Settings), nil
}

// validateOverrides checks a chapter's or lesson's partial settings: unset
// (zero) fields are fine, set ones must be valid.
func validateOverrides(s domain.OutputSettings) error {
	if s.Depth != "" || s.Level != "" || s.Minutes != 0 {
		full := s
		if full.Depth == "" {
			full.Depth = "standard"
		}
		if full.Level == "" {
			full.Level = "intermediate"
		}
		if full.Minutes == 0 {
			full.Minutes = 50
		}
		return validateOutputSettings(full)
	}
	return validateOutputSettings(NormalizeSettings(s))
}

// SaveCourse changes a course's name, description and default settings.
func (a *App) SaveCourse(ctx context.Context, owner, id string, version int, name, description string, settings domain.OutputSettings) (domain.Record, error) {
	name = strings.TrimSpace(name)
	settings = NormalizeSettings(settings)
	if err := validateOutputSettings(settings); err != nil {
		return domain.Record{}, err
	}
	return set[domain.Course](ctx, a, owner, id, version, func(c *domain.Course) error {
		if len([]rune(name)) == 0 || len([]rune(name)) > 150 {
			return errors.New("course_name_required")
		}
		c.Name = name
		c.Description = description
		c.Settings = settings
		return nil
	})
}
