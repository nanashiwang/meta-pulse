package marketplace

import (
	"errors"
	"testing"
)

func validProject() Entry {
	return Entry{QuestionID: "12", Kind: "project", Summary: "A small prompt workspace", UseCase: "development", Relation: "self", GitHubURL: "https://github.com/example/prompt-board.git", TechStack: "Go, React", Pricing: "free", DataNote: "Requests go to the author's demo service and are not stored.", AccessNote: "No login; the author pays the demo AI cost."}
}

func TestValidateEntryProjectShapes(t *testing.T) {
	for _, entry := range []Entry{validProject(), {QuestionID: "13", Kind: "project", Summary: "A browser demo", UseCase: "productivity", Relation: "self", DemoURL: "https://demo.example.com/app", Pricing: "free", DataNote: "Input is processed by the demo provider.", AccessNote: "No account required."}, {QuestionID: "14", Kind: "idea", Summary: "Discuss a workflow idea", UseCase: "experiment", Relation: "self", IdeaStage: "feedback"}} {
		got, err := ValidateEntry(entry, true)
		if err != nil {
			t.Fatalf("entry rejected: %+v: %v", entry, err)
		}
		if got.QuestionID == "" {
			t.Fatal("question ID was lost")
		}
	}
}

func TestValidateEntryRejectsUnsafeOrIncompleteLinks(t *testing.T) {
	for _, entry := range []Entry{
		{QuestionID: "1", Kind: "project", Summary: "Unsafe", UseCase: "other", Relation: "self", GitHubURL: "http://github.com/example/repo"},
		{QuestionID: "1", Kind: "project", Summary: "Wrong host", UseCase: "other", Relation: "self", GitHubURL: "https://gitlab.com/example/repo"},
		{QuestionID: "1", Kind: "project", Summary: "Query", UseCase: "other", Relation: "self", GitHubURL: "https://github.com/example/repo?token=secret"},
		{QuestionID: "1", Kind: "project", Summary: "No link", UseCase: "other", Relation: "self"},
		{QuestionID: "1", Kind: "project", Summary: "Private host", UseCase: "other", Relation: "self", DemoURL: "https://127.0.0.1/app"},
	} {
		if !errors.Is(func() error { _, err := ValidateEntry(entry, true); return err }(), ErrInvalid) {
			t.Fatalf("unsafe input accepted: %+v", entry)
		}
	}
}

func TestValidateEntryNormalizesGitHubURL(t *testing.T) {
	entry := validProject()
	entry.GitHubURL = "https://www.github.com/owner/repo.git"
	got, err := ValidateEntry(entry, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.GitHubURL != "https://github.com/owner/repo" {
		t.Fatalf("github=%q", got.GitHubURL)
	}
}

func TestSafeURLRejectsLocalAndCredentialBearingURLs(t *testing.T) {
	for _, raw := range []string{"http://example.com/app", "https://example.com/app?key=1", "https://user:pass@example.com/app", "https://localhost/app", "https://example.internal/app", "https://192.0.2.1/app", "https://127.1/app", "https://0x7f.0x1/app", "https://metarhub.com/app", "https://demo.example.com/app#token"} {
		if _, err := SafeURL(raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
}
