package shared

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BrainerVirus/youtrack-cli/internal/youtrack/adapter"
)

// ReadFiles reads files to attach, all before any request, so a missing or
// unreadable file posts nothing. Each attachment is named after its file's
// base name, which must be unique: a comment refers to it by that name.
func ReadFiles(paths []string) ([]adapter.File, error) {
	out := make([]adapter.File, 0, len(paths))
	seen := map[string]string{}
	for _, p := range paths {
		name := filepath.Base(p)
		if prev, ok := seen[name]; ok {
			return nil, fmt.Errorf("cannot attach %s and %s: both are named %s", prev, p, name)
		}
		seen[name] = p
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("cannot attach %s: %w", p, err)
		}
		out = append(out, adapter.File{Name: name, MimeType: mediaType(name, data), Data: data})
	}
	return out, nil
}

// mediaType guesses a file's media type from its extension, else from its
// content, without parameters such as charset.
func mediaType(name string, data []byte) string {
	t := mime.TypeByExtension(filepath.Ext(name))
	if t == "" {
		t = http.DetectContentType(data)
	}
	if mt, _, err := mime.ParseMediaType(t); err == nil {
		return mt
	}
	return "application/octet-stream"
}

// InlineRefs rewrites Markdown links and images in body that point at an
// attached file by the path it was given, such as ![](shots/a.png), to its
// attachment name (![](a.png)), which YouTrack resolves to the attachment.
func InlineRefs(body string, paths []string) string {
	for _, p := range paths {
		if name := filepath.Base(p); name != p {
			body = strings.ReplaceAll(body, "]("+p+")", "]("+name+")")
		}
	}
	return body
}
