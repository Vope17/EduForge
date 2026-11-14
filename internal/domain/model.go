// Package domain defines persisted contracts independently of transport and UI language.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not_found")
var ErrConflict = errors.New("conflict")
var ErrPaused = errors.New("providers_unavailable")

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Now() string            { return time.Now().UTC().Format(time.RFC3339Nano) }
func Language(s string) bool { return s == "en" || s == "zh-TW" }
func Encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func Decode[T any](b json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

type Record struct {
	ID      string          `json:"id"`
	Owner   string          `json:"owner_user_id"`
	Kind    string          `json:"kind"`
	Parent  string          `json:"parent_id,omitempty"`
	Project string          `json:"project_id,omitempty"`
	Paper   string          `json:"paper_id,omitempty"`
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
	Created string          `json:"created_at"`
	Updated string          `json:"updated_at"`
}
type Project struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Style       map[string]Style `json:"style"`
}

// Folder groups studies in a project's research library (one level).
type Folder struct {
	Name string `json:"name"`
}
type Paper struct {
	Title string `json:"title"`
	Repo  string `json:"repo"`
	Group string `json:"group"`
	// Folder is the library folder holding the study; empty is unfiled.
	Folder      string         `json:"folder,omitempty"`
	KnowledgeID string         `json:"knowledge_id"`
	Settings    OutputSettings `json:"settings"`
}
type OutputSettings struct {
	Selected   []string          `json:"selected"`
	Languages  map[string]string `json:"languages"`
	Depth      string            `json:"depth"`
	Level      string            `json:"level"`
	Audience   string            `json:"audience"`
	Minutes    int               `json:"minutes"`
	TemplateID string            `json:"template_id"`
	// ReaderTemplateID is an imported Word template for readers (A).
	ReaderTemplateID string `json:"reader_template_id,omitempty"`
	// Instructions are the user's own wishes for each kind of output
	// ("A", "B"), passed to the model as written.
	Instructions map[string]string `json:"instructions,omitempty"`
}
type Unit struct {
	ID         string `json:"id"`
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name,omitempty"`
	// SourceTitle is the document's own title (PDF metadata) when it has one.
	SourceTitle string `json:"source_title,omitempty"`
	Locator     int    `json:"locator"`
	Kind        string `json:"kind"`
	Text        string `json:"text"`
	Preview     string `json:"preview,omitempty"`
}
type Image struct {
	Path     string `json:"path"`
	Citation string `json:"citation"`
	Original bool   `json:"original"`
	Caption  string `json:"caption,omitempty"`
}
type Issue struct {
	Code     string `json:"code"`
	Citation string `json:"citation,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Index    int    `json:"index,omitempty"`
	// Resolved marks a parse-time review item (e.g. formula_review) as checked
	// by the user. Only knowledge issues can be resolved; artifact checks skip them.
	Resolved bool `json:"resolved,omitempty"`
}
type Parsed struct {
	// ParserVersion records which parser produced this result; older results
	// are re-parsed so parser fixes (e.g. figure extraction) take effect.
	ParserVersion int `json:"parser_version,omitempty"`
	// Title is the document's own title, e.g. from PDF metadata.
	Title  string  `json:"title,omitempty"`
	Units  []Unit  `json:"units"`
	Images []Image `json:"images"`
	Issues []Issue `json:"issues"`
}
type Source struct {
	Name   string  `json:"name"`
	Role   string  `json:"role"`
	Path   string  `json:"path"`
	SHA256 string  `json:"sha256"`
	Size   int64   `json:"size"`
	Parsed *Parsed `json:"parsed,omitempty"`
	Error  string  `json:"error,omitempty"`
}
type Claim struct {
	Section   int      `json:"section"`
	Text      string   `json:"text"`
	Citations []string `json:"citations"`
	Verified  bool     `json:"verified"`
	Kind      string   `json:"kind"` // source_excerpt (rules) or ai_summary
	// Unsupported lists numbers in an AI-written point that its cited
	// passages do not contain.
	Unsupported []string `json:"unsupported,omitempty"`
}
type Term struct {
	Original     string   `json:"original"`
	English      string   `json:"en"`
	Chinese      string   `json:"zh-TW"`
	Abbreviation string   `json:"abbreviation"`
	Definition   string   `json:"definition"`
	Citations    []string `json:"citations"`
	Context      string   `json:"context"`
	Deprecated   []string `json:"deprecated,omitempty"`
}
type Conditions struct {
	Task       string `json:"task"`
	Dataset    string `json:"dataset"`
	Split      string `json:"split"`
	Metric     string `json:"metric"`
	Evaluation string `json:"evaluation"`
}
type Knowledge struct {
	Title      string     `json:"title"`
	Claims     []Claim    `json:"claims"`
	Terms      []Term     `json:"terms"`
	Units      []Unit     `json:"units"`
	Images     []Image    `json:"images"`
	Issues     []Issue    `json:"issues"`
	Conditions Conditions `json:"conditions"`
	Confirmed  bool       `json:"confirmed"`
	Edits      int        `json:"edits"`
	// Author is "ai" when a model wrote the claims, terms and conditions;
	// empty for rule-based source excerpts. AIError says why a model was
	// not used when one was tried.
	Author   string `json:"author,omitempty"`
	Language string `json:"language,omitempty"`
	AIError  string `json:"ai_error,omitempty"`
}

// Style deliberately excludes language, facts, commands, metrics and permissions.
type Style map[string]any
type Profile struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Current string `json:"current_version_id"`
}
type ProfileVersion struct {
	ProfileID string           `json:"profile_id"`
	Common    Style            `json:"common"`
	Outputs   map[string]Style `json:"outputs"`
	Confirmed bool             `json:"confirmed"`
	Origin    string           `json:"origin"`
}
type StyleSnapshot struct {
	Schema           int              `json:"schema"`
	ProfileID        string           `json:"profile_id,omitempty"`
	ProfileVersionID string           `json:"profile_version_id,omitempty"`
	Resolved         map[string]Style `json:"resolved"`
}
type Feedback struct {
	TargetID     string           `json:"target_id"`
	Text         string           `json:"text"`
	OriginalText string           `json:"original_text,omitempty"`
	Disabled     bool             `json:"disabled,omitempty"`
	Changes      map[string]Style `json:"changes"`
	Scope        string           `json:"scope"`
	Adopted      bool             `json:"adopted"`
	ProfileID    string           `json:"profile_id,omitempty"`
}
type Provider struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Model         string   `json:"model"`
	Enabled       bool     `json:"enabled"`
	Paid          bool     `json:"paid"`
	InputPrice    *float64 `json:"input_price"`
	OutputPrice   *float64 `json:"output_price"`
	MaxOutput     int      `json:"max_output_tokens"`
	HasCredential bool     `json:"has_credential"`
}
type ProviderRef struct {
	ID       string   `json:"id"`
	Provider Provider `json:"provider"`
}
type Request struct {
	PaperIDs         []string           `json:"paper_ids"`
	UsePaperSettings bool               `json:"use_paper_settings,omitempty"`
	Targets          []GenerationTarget `json:"targets,omitempty"`
	Selected         []string           `json:"selected"`
	Languages        map[string]string  `json:"languages"`
	UILanguage       string             `json:"ui_language"`
	Settings         OutputSettings     `json:"settings"`
	ProfileID        string             `json:"profile_id"`
	Overrides        map[string]Style   `json:"overrides"`
	AllowPaid        bool               `json:"allow_paid"`
	Unlimited        bool               `json:"unlimited"`
	Budget           *float64           `json:"budget"`
	Spent            float64            `json:"spent"`
	Providers        []ProviderRef      `json:"providers"`
	Status           string             `json:"status"`
	PreviewIDs       []string           `json:"preview_ids"`
	Started          string             `json:"started_at"`
	Style            StyleSnapshot      `json:"style_snapshot"`
}
type GenerationTarget struct {
	PaperID       string         `json:"paper_id"`
	KnowledgeID   string         `json:"knowledge_id"`
	Settings      OutputSettings `json:"settings"`
	TemplateStyle Style          `json:"template_style,omitempty"`
	// ReaderTemplateStyle is the Word template style for readers.
	ReaderTemplateStyle Style `json:"reader_template_style,omitempty"`
}
type Task struct {
	RequestID        string         `json:"request_id"`
	Type             string         `json:"type"`
	Status           string         `json:"status"`
	Stage            string         `json:"stage"`
	Language         string         `json:"language"`
	KnowledgeID      string         `json:"knowledge_id"`
	RelatedKnowledge []string       `json:"related_knowledge_ids,omitempty"`
	GroupedPapers    []string       `json:"grouped_paper_ids,omitempty"`
	Settings         OutputSettings `json:"settings"`
	Style            StyleSnapshot  `json:"style_snapshot"`
	ReadTerms        []string       `json:"read_terms"`
	Error            string         `json:"error,omitempty"`
	ResultID         string         `json:"result_id,omitempty"`
	Completed        int            `json:"completed"`
	Total            int            `json:"total"`
}
type Step struct {
	TaskID   string  `json:"task_id"`
	Key      string  `json:"key"`
	Content  Section `json:"content"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Language string  `json:"language"`
	Cost     float64 `json:"cost"`
	Mode     string  `json:"mode"`
}
type Section struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Notes     string   `json:"notes,omitempty"`
	Citations []string `json:"citations"`
	Image     string   `json:"image,omitempty"`
	// Figures lists the original figures shown with a reader section, in
	// order. nil means "assign automatically from cited pages"; an empty
	// list means none.
	Figures []string `json:"figures"`
	// Layout holds a slide's editable text and image boxes. Empty for readers
	// and for slides made before layouts existed.
	Layout []Element `json:"layout,omitempty"`
	// Slides is a presentation topic written as several slides (the model's
	// reply for one section of a deck); the deck is built from them.
	Slides []Section `json:"slides,omitempty"`
	// Equation is a slide's key formula in TeX, shown above its points.
	Equation string `json:"equation,omitempty"`
	// Part is the paper section a slide presents (decks follow the paper).
	Part string `json:"part,omitempty"`
}

// Element is a positioned box on a slide. Coordinates are fractions of the
// slide width/height (0–1) so a layout survives a 16:9 ↔ 4:3 change.
type Element struct {
	ID   string  `json:"id"`
	Kind string  `json:"kind"` // "text", "image" or "formula" (Tex)
	Role string  `json:"role,omitempty"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Text string  `json:"text,omitempty"`
	Path string  `json:"path,omitempty"`
	Size float64 `json:"size,omitempty"` // font size in points
	Bold bool    `json:"bold,omitempty"`
	// Align is "center" or "right"; empty is left.
	Align string `json:"align,omitempty"`
	// Tex is a formula box's TeX, drawn as a picture.
	Tex string `json:"tex,omitempty"`
}
type Check struct {
	Status    string  `json:"status"`
	Issues    []Issue `json:"issues"`
	CheckedAt string  `json:"checked_at"`
}
type Artifact struct {
	Type             string         `json:"type"`
	Language         string         `json:"language"`
	KnowledgeID      string         `json:"knowledge_id"`
	RelatedKnowledge []string       `json:"related_knowledge_ids,omitempty"`
	GroupedPapers    []string       `json:"grouped_paper_ids,omitempty"`
	RequestID        string         `json:"request_id"`
	TaskID           string         `json:"task_id"`
	Style            StyleSnapshot  `json:"style_snapshot"`
	Settings         OutputSettings `json:"settings"`
	Sections         []Section      `json:"sections"`
	SuggestedTarget  *Section       `json:"suggested_target,omitempty"`
	Mode             string         `json:"mode"`
	Check            Check          `json:"check"`
	Edits            int            `json:"edits"`
	EditingSeconds   float64        `json:"editing_seconds,omitempty"`
	Previous         string         `json:"previous_id,omitempty"`
	EstimatedMinutes float64        `json:"estimated_minutes"`
	CreatedFrom      string         `json:"created_from,omitempty"`
}
type Template struct {
	Name        string   `json:"name"`
	SourceID    string   `json:"source_id"`
	Description string   `json:"description"`
	Background  string   `json:"background"`
	Foreground  string   `json:"foreground"`
	Accent      string   `json:"accent"`
	Font        string   `json:"font"`
	Ratio       string   `json:"ratio"`
	LogoImage   string   `json:"logo_image"`
	Fixed       []string `json:"fixed_elements"`
	Preview     string   `json:"preview"`
	Confirmed   bool     `json:"confirmed"`
	Issues      []Issue  `json:"issues"`
	// Decor is the title decoration: bar, line, rule or band.
	Decor string `json:"decor,omitempty"`
	// TitleColor is the title text colour.
	TitleColor string `json:"title_color,omitempty"`

	// Kind is "pptx" (slides) or "docx" (readers); empty means pptx.
	Kind string `json:"kind,omitempty"`
	// An imported PowerPoint template: the stored file, its slide size in EMU,
	// the layouts used for cover and content slides, where titles and content
	// go (fractions x, y, w, h), its fonts and rendered layout backgrounds.
	File             string    `json:"file,omitempty"`
	Width            int64     `json:"width,omitempty"`
	Height           int64     `json:"height,omitempty"`
	CoverLayout      string    `json:"cover_layout,omitempty"`
	ContentLayout    string    `json:"content_layout,omitempty"`
	CoverPlaceholder string    `json:"cover_placeholder,omitempty"`
	CoverSubtitleIdx string    `json:"cover_subtitle_idx,omitempty"`
	ContentNumberIdx string    `json:"content_number_idx,omitempty"`
	TitleBox         []float64 `json:"title_box,omitempty"`
	BodyBox          []float64 `json:"body_box,omitempty"`
	CoverTitleBox    []float64 `json:"cover_title_box,omitempty"`
	CoverSubtitleBox []float64 `json:"cover_subtitle_box,omitempty"`
	HeadingFont      string    `json:"heading_font,omitempty"`
	TitleAlign       string    `json:"title_align,omitempty"`
	CoverAlign       string    `json:"cover_align,omitempty"`
	CoverImage       string    `json:"cover_image,omitempty"`
	ContentImage     string    `json:"content_image,omitempty"`
	// History holds earlier versions (newest first) for restoring.
	History []TemplateVersion `json:"history,omitempty"`
	// LayoutTypes names what each layout is for (cover, content, …), when
	// the template was extracted from a presentation.
	LayoutTypes map[string]string `json:"layout_types,omitempty"`
	// Draft is a template still being extracted from a presentation; it has
	// no File until the user confirms the extraction.
	Draft      bool                `json:"draft,omitempty"`
	Extraction *TemplateExtraction `json:"extraction,omitempty"`
}

// TemplateExtraction is the analysis of an uploaded presentation and the
// suggested template, awaiting the user's review.
type TemplateExtraction struct {
	Source     string         `json:"source"` // the uploaded deck
	SourceName string         `json:"source_name"`
	Analysis   DeckAnalysis   `json:"analysis"`
	Suggestion DeckSuggestion `json:"suggestion"`
	UsedAI     bool           `json:"used_ai"`
	AIError    string         `json:"ai_error,omitempty"`
	Thumbs     []string       `json:"thumbs"` // slide images, in slide order
}

// DeckAnalysis describes a presentation's slides, the layouts they use and
// the shapes that look like design (repeated or on the cover only).
type DeckAnalysis struct {
	Width       int64            `json:"width"`
	Height      int64            `json:"height"`
	Slides      []DeckSlide      `json:"slides"`
	Layouts     []DeckLayout     `json:"layouts"`
	Decorations []DeckDecoration `json:"decorations"`
	// Skipped lists text that sits in the same place on several slides but
	// changes from slide to slide (section labels, page numbers, sources):
	// slide content, not design.
	Skipped []string `json:"skipped"`
	// Style is the design measured from the slides themselves, for decks
	// that draw everything on the slides instead of using placeholders.
	Style *DeckStyle `json:"style,omitempty"`
}

// DeckStyle is a deck's design read from its slides: theme colour slots,
// fonts and the title/content areas (boxes are x, y, width, height as
// fractions of the slide).
type DeckStyle struct {
	Colors           map[string]string `json:"colors"`
	HeadingFont      string            `json:"heading_font,omitempty"`
	BodyFont         string            `json:"body_font,omitempty"`
	HeadingFontEA    string            `json:"heading_font_ea,omitempty"`
	BodyFontEA       string            `json:"body_font_ea,omitempty"`
	TitleBox         []float64         `json:"title_box,omitempty"`
	BodyBox          []float64         `json:"body_box,omitempty"`
	CoverTitleBox    []float64         `json:"cover_title_box,omitempty"`
	CoverSubtitleBox []float64         `json:"cover_subtitle_box,omitempty"`
}
type DeckSlide struct {
	N      int      `json:"n"`
	Layout string   `json:"layout"`
	Texts  []string `json:"texts"`
}
type DeckLayout struct {
	Part         string   `json:"part"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Slides       []int    `json:"slides"`
	Placeholders []string `json:"placeholders"`
}
type DeckDecoration struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // shape, picture, line, text, group
	Name    string    `json:"name"`
	Box     []float64 `json:"box"`
	Color   string    `json:"color,omitempty"`
	Text    string    `json:"text,omitempty"`
	Slides  []int     `json:"slides"`
	Default bool      `json:"default"`
}

// DeckSuggestion is what to make of a presentation: the style's name and
// description, the cover and content layouts, each layout's role and the
// decorations to keep.
type DeckSuggestion struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	CoverLayout   string            `json:"cover_layout"`
	ContentLayout string            `json:"content_layout"`
	LayoutTypes   map[string]string `json:"layout_types"`
	Decorations   []string          `json:"decorations"`
	// ApplyStyle writes the measured DeckStyle into the template.
	ApplyStyle bool `json:"apply_style"`
}

// TemplateVersion is an earlier state of a template: its record version,
// when it was replaced, and its full data without history.
type TemplateVersion struct {
	Version  int             `json:"version"`
	SavedAt  string          `json:"saved_at"`
	Template json.RawMessage `json:"template"`
}
