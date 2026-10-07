package prompter

import (
	"bytes"
	"errors"
	"io"
	"os"
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

func TestConfirmIsNoUnlessTheAnswerIsYes(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "YES\n": true, "\n": false, "n\n": false, "maybe\ny\n": true} {
		got, err := New(strings.NewReader(input), &bytes.Buffer{}).Confirm("Delete?")
		if err != nil || got != want {
			t.Errorf("Confirm(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
}

func TestReadHidden(t *testing.T) {
	t.Run("given Ctrl-C while reading, it restores the terminal and cancels", func(t *testing.T) {
		block := make(chan struct{})
		defer close(block)
		restored := false
		interrupt := make(chan os.Signal, 1)
		interrupt <- os.Interrupt

		_, err := readHidden(func() ([]byte, error) { <-block; return nil, nil }, func() { restored = true }, interrupt)

		if !errors.Is(err, clierr.ErrCancel) {
			t.Errorf("err = %v, want ErrCancel", err)
		}
		if !restored {
			t.Error("terminal state was not restored")
		}
	})

	t.Run("given end of input, it cancels", func(t *testing.T) {
		_, err := readHidden(func() ([]byte, error) { return nil, io.EOF }, func() {}, make(chan os.Signal))
		if !errors.Is(err, clierr.ErrCancel) {
			t.Errorf("err = %v, want ErrCancel", err)
		}
	})

	t.Run("given a typed secret, it returns it without restoring", func(t *testing.T) {
		restored := false
		b, err := readHidden(func() ([]byte, error) { return []byte("perm-x"), nil }, func() { restored = true }, make(chan os.Signal))
		if string(b) != "perm-x" || err != nil || restored {
			t.Errorf("got %q, %v, restored=%t", b, err, restored)
		}
	})
}
