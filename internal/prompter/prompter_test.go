package prompter

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/clierr"
)

func TestInputFallsBackToDefaultOnEmptyAnswer(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("\n"), &out)
	got, err := p.Input("URL:", "https://acme.youtrack.cloud")
	if err != nil || got != "https://acme.youtrack.cloud" {
		t.Fatalf("Input = %q, %v", got, err)
	}
}

func TestPasswordTrimsThePastedToken(t *testing.T) {
	p := New(strings.NewReader("  perm-abc  \n"), &bytes.Buffer{})
	got, err := p.Password("Token:")
	if err != nil || got != "perm-abc" {
		t.Fatalf("Password = %q, %v", got, err)
	}
}

func TestEndOfInputCancels(t *testing.T) {
	p := New(strings.NewReader(""), &bytes.Buffer{})
	if _, err := p.Input("URL:", ""); !errors.Is(err, clierr.ErrCancel) {
		t.Fatalf("err = %v, want ErrCancel", err)
	}
}

func TestSelectRepromptsUntilTheAnswerIsInRange(t *testing.T) {
	var out bytes.Buffer
	p := New(strings.NewReader("9\nabc\n2\n"), &out)
	got, err := p.Select("Host:", []string{"a", "b"}, 0)
	if err != nil || got != 1 {
		t.Fatalf("Select = %d, %v", got, err)
	}
	if n := strings.Count(out.String(), "Enter a number between 1 and 2"); n != 2 {
		t.Errorf("expected 2 re-prompts, got %d in %q", n, out.String())
	}
}
