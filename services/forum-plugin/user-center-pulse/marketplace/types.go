// Package marketplace extends Answer posts with reviewed project metadata.
// Answer remains the only identity, body and interaction source of truth.
package marketplace

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalid   = errors.New("invalid_request")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	ErrNotFound  = errors.New("not_found")
	ErrLimit     = errors.New("rate_limited")
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
var githubSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var hostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
var numericHostTail = regexp.MustCompile(`^(?:[0-9]+|0x[0-9a-f]+)$`)

// Entry contains only discovery/run metadata; post bodies stay in Answer.
type Entry struct {
	QuestionID     string `json:"question_id"`
	Kind           string `json:"kind"`
	Summary        string `json:"summary"`
	UseCase        string `json:"use_case"`
	GitHubURL      string `json:"github_url"`
	DemoURL        string `json:"demo_url"`
	Relation       string `json:"relation"`
	OriginalAuthor string `json:"original_author"`
	License        string `json:"license"`
	TechStack      string `json:"tech_stack"`
	Pricing        string `json:"pricing"`
	DataNote       string `json:"data_note"`
	AccessNote     string `json:"access_note"`
	EmbedRequested bool   `json:"embed_requested"`
	SourceIdeaID   string `json:"source_idea_id"`
	IdeaStage      string `json:"idea_stage"`
}

type Author struct {
	Username string `json:"username"`
	Name     string `json:"display_name"`
	Avatar   string `json:"avatar"`
}

type Item struct {
	ID           string `json:"id"`
	Entry        Entry  `json:"entry"`
	Title        string `json:"title"`
	Author       Author `json:"author"`
	AnswerCount  int    `json:"answer_count"`
	ViewCount    int    `json:"view_count"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
	ReviewNote   string `json:"review_note,omitempty"`
	EmbedAllowed bool   `json:"embed_allowed"`
	DemoStatus   string `json:"demo_status"`
	Featured     bool   `json:"featured"`
	HasPublished bool   `json:"has_published"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
	PublishedAt  int64  `json:"published_at"`
}

type Detail struct {
	Item     Item   `json:"item"`
	Source   *Item  `json:"source,omitempty"`
	Projects []Item `json:"projects"`
}

type Command struct {
	Op           string `json:"op"`
	ID           string `json:"id"`
	Version      int64  `json:"version"`
	Entry        Entry  `json:"entry"`
	Decision     string `json:"decision"`
	Reason       string `json:"reason"`
	EmbedAllowed bool   `json:"embed_allowed"`
	DemoStatus   string `json:"demo_status"`
	Featured     bool   `json:"featured"`
}

type Query struct {
	Kind       string
	Text       string
	UseCase    string
	Capability string
	Order      string
	Status     string
	Page       int
}
type Page struct {
	Items    []Item `json:"items"`
	Count    int    `json:"count"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func validID(value string) bool {
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == value
}
func ValidStatus(v string) bool {
	return v == "draft" || v == "pending_review" || v == "published" || v == "changes_requested" || v == "archived"
}
func validUseCase(v string) bool {
	return v == "productivity" || v == "content" || v == "development" || v == "education" || v == "experiment" || v == "other"
}
func validStage(v string) bool {
	return v == "feedback" || v == "building" || v == "launched" || v == "paused"
}

// No network fetching or credential-bearing URL is needed for catalog links.
// Literal/local destinations and METAR's own origin cannot become embeds.
func SafeURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 512 || strings.ContainsAny(value, "\\\r\n\t ") {
		return "", ErrInvalid
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if u.Port() != "" && u.Port() != "443" {
		return "", ErrInvalid
	}
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") || !hostPattern.MatchString(host) || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return "", ErrInvalid
	}
	// Browsers also interpret shortened and hexadecimal IPv4 addresses.
	if numericHostTail.MatchString(host[strings.LastIndex(host, ".")+1:]) {
		return "", ErrInvalid
	}
	for _, suffix := range []string{"localhost", ".localhost", ".local", ".internal", ".lan", ".test", ".invalid", ".home.arpa"} {
		if host == suffix || strings.HasSuffix(host, suffix) {
			return "", ErrInvalid
		}
	}
	if host == "metarhub.com" || strings.HasSuffix(host, ".metarhub.com") || host == "metar.uk" || strings.HasSuffix(host, ".metar.uk") {
		return "", ErrInvalid
	}
	u.Host = host
	return u.String(), nil
}
func GitHubURL(raw string) (string, error) {
	value, err := SafeURL(raw)
	if err != nil || value == "" {
		return value, err
	}
	u, _ := url.Parse(value)
	if u.Host != "github.com" && u.Host != "www.github.com" {
		return "", ErrInvalid
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !githubSegment.MatchString(parts[0]) || !githubSegment.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." {
		return "", ErrInvalid
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	if repo == "" || repo == "." || repo == ".." {
		return "", ErrInvalid
	}
	return "https://github.com/" + parts[0] + "/" + repo, nil
}
func ValidateEntry(v Entry, submitting bool) (Entry, error) {
	v.QuestionID = strings.TrimSpace(v.QuestionID)
	if !validID(v.QuestionID) || (v.Kind != "project" && v.Kind != "idea") {
		return Entry{}, ErrInvalid
	}
	fields := []struct {
		p   *string
		max int
	}{{&v.Summary, 280}, {&v.OriginalAuthor, 120}, {&v.License, 64}, {&v.TechStack, 255}, {&v.DataNote, 500}, {&v.AccessNote, 300}}
	for _, f := range fields {
		*f.p = strings.TrimSpace(*f.p)
		if !utf8.ValidString(*f.p) || utf8.RuneCountInString(*f.p) > f.max {
			return Entry{}, ErrInvalid
		}
	}
	if !validUseCase(v.UseCase) || (submitting && utf8.RuneCountInString(v.Summary) < 5) {
		return Entry{}, ErrInvalid
	}
	var err error
	v.GitHubURL, err = GitHubURL(v.GitHubURL)
	if err != nil {
		return Entry{}, err
	}
	v.DemoURL, err = SafeURL(v.DemoURL)
	if err != nil {
		return Entry{}, err
	}
	if v.Relation != "self" && v.Relation != "recommendation" {
		return Entry{}, ErrInvalid
	}
	if v.Relation == "recommendation" && submitting && v.OriginalAuthor == "" {
		return Entry{}, ErrInvalid
	}
	if v.SourceIdeaID != "" && (!validID(v.SourceIdeaID) || v.Kind != "project") {
		return Entry{}, ErrInvalid
	}
	if v.Kind == "project" {
		if submitting && v.GitHubURL == "" && v.DemoURL == "" {
			return Entry{}, ErrInvalid
		}
		if v.Pricing != "" && v.Pricing != "free" && v.Pricing != "freemium" && v.Pricing != "paid" {
			return Entry{}, ErrInvalid
		}
		if v.DemoURL != "" && submitting && (v.Pricing == "" || v.DataNote == "" || v.AccessNote == "") {
			return Entry{}, ErrInvalid
		}
		if v.EmbedRequested && v.DemoURL == "" {
			return Entry{}, ErrInvalid
		}
		v.IdeaStage = ""
	} else {
		if !validStage(v.IdeaStage) || v.GitHubURL != "" || v.DemoURL != "" || v.EmbedRequested || v.Pricing != "" {
			return Entry{}, ErrInvalid
		}
	}
	return v, nil
}
func (q Query) Validate(admin bool) error {
	if q.Page < 1 || q.Page > 1000 || utf8.RuneCountInString(q.Text) > 80 || (q.Kind != "" && q.Kind != "project" && q.Kind != "idea") || (q.UseCase != "" && !validUseCase(q.UseCase)) || (q.Capability != "" && q.Capability != "demo" && q.Capability != "github") || (q.Order != "" && q.Order != "newest" && q.Order != "featured") || (q.Status != "" && (!admin || !ValidStatus(q.Status))) {
		return ErrInvalid
	}
	return nil
}
