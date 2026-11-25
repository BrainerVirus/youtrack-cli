package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/transport"
)

func TestSafeName(t *testing.T) {
	for name, want := range map[string]string{
		"trace.log":     "trace.log",
		"../evil.txt":   ".._evil.txt",
		`C:\x`:          "C__x",
		"a. ":           "a__",
		"report.":       "report_",
		"notes ":        "notes_",
		".":             "_",
		"..":            "__",
		"":              "_",
		"NUL.txt":       "_NUL.txt",
		"nul":           "_nul",
		"Com1.tar.gz":   "_Com1.tar.gz",
		"lpt9":          "_lpt9",
		"COM¹.log":      "_COM¹.log",
		"CON .txt":      "_CON .txt",
		"conin$":        "_conin$",
		"NULL.txt":      "NULL.txt",
		"com10":         "com10",
		"nul.":          "nul_",
		`a<b>c"d|e?f*g`: "a_b_c_d_e_f_g",
		"tab\there":     "tab_here",
	} {
		if got := safeName(name); got != want {
			t.Errorf("safeName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFileNameReason(t *testing.T) {
	for name, want := range map[string]string{
		"ok.txt":      "",
		"../x":        "attachment names cannot hold a path",
		"NUL.txt":     "the name is not a valid file name on every system",
		"a. ":         "the name is not a valid file name on every system",
		`dir\sub.txt`: "attachment names cannot hold a path",
	} {
		if _, why := fileName(adapter.IssueAttachment{ID: "8-1", Name: name}, map[string]bool{}); why != want {
			t.Errorf("fileName(%q) why = %q, want %q", name, why, want)
		}
	}
}

// halfServer writes the first bytes of a file, then calls interrupt and
// holds the response open until the client goes away.
func halfServer(t *testing.T, interrupt func()) *transport.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial-bytes"))
		w.(http.Flusher).Flush()
		interrupt()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return transport.New(transport.Options{BaseURL: srv.URL})
}

func assertNoFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("left %s behind", e.Name())
	}
}

func TestSaveInterrupted(t *testing.T) {
	a := adapter.IssueAttachment{ID: "8-1", Name: "big.bin", URL: "/api/files/8-1"}

	t.Run("given the context is cancelled mid-body, it removes the partial file, with or without --clobber", func(t *testing.T) {
		for _, clobber := range []bool{false, true} {
			ctx, cancel := context.WithCancel(context.Background())
			client := halfServer(t, cancel)
			dir := t.TempDir()
			if _, err := save(ctx, client, a, filepath.Join(dir, "big.bin"), clobber); err == nil {
				t.Fatalf("clobber %v: save succeeded after a cancel", clobber)
			}
			assertNoFiles(t, dir)
		}
	})

	t.Run("given Ctrl-C mid-body, the interruptible context cancels and the partial file is removed", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("os.Process.Signal cannot send os.Interrupt on Windows")
		}
		ctx, stop := interruptible(context.Background())
		defer stop()
		client := halfServer(t, func() {
			p, err := os.FindProcess(os.Getpid())
			if err == nil {
				err = p.Signal(os.Interrupt)
			}
			if err != nil {
				t.Error(err)
			}
		})
		dir := t.TempDir()
		if _, err := save(ctx, client, a, filepath.Join(dir, "big.bin"), false); err == nil || ctx.Err() == nil {
			t.Fatalf("save err = %v, ctx err = %v; want both set", err, ctx.Err())
		}
		assertNoFiles(t, dir)
	})
}
