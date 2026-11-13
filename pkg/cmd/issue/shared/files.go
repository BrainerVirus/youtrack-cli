package shared

import (
	"errors"
	"fmt"
	"io"
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
	names, err := attachmentNames(paths)
	if err != nil {
		return nil, err
	}
	out := make([]adapter.File, 0, len(paths))
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("cannot attach %s: %w", p, err)
		}
		out = append(out, adapter.File{Name: names[i], MimeType: mediaType(names[i], data), Data: data})
	}
	return out, nil
}

// sniffLen is how much of a file http.DetectContentType looks at.
const sniffLen = 512

// CheckFiles checks files to upload without reading them into memory: each
// must be a regular file that opens and reads, all before any request, so a
// missing or unreadable file posts nothing. It records each file's size and
// media type, guessed from its name or its first bytes. Names follow
// ReadFiles.
func CheckFiles(paths []string) ([]adapter.LocalFile, error) {
	names, err := attachmentNames(paths)
	if err != nil {
		return nil, err
	}
	out := make([]adapter.LocalFile, 0, len(paths))
	for i, p := range paths {
		size, head, err := statAndSniff(p)
		if err != nil {
			return nil, fmt.Errorf("cannot attach %s: %w", p, err)
		}
		out = append(out, adapter.LocalFile{Name: names[i], MimeType: mediaType(names[i], head), Path: p, Size: size})
	}
	return out, nil
}

func statAndSniff(path string) (size int64, head []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}
	if !st.Mode().IsRegular() {
		return 0, nil, errors.New("not a regular file")
	}
	head = make([]byte, sniffLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, nil, err
	}
	return st.Size(), head[:n], nil
}

// attachmentNames returns each path's base name, failing when two share one.
func attachmentNames(paths []string) ([]string, error) {
	names := make([]string, len(paths))
	seen := map[string]string{}
	for i, p := range paths {
		name := filepath.Base(p)
		if prev, ok := seen[name]; ok {
			return nil, fmt.Errorf("cannot attach %s and %s: both are named %s", prev, p, name)
		}
		seen[name] = p
		names[i] = name
	}
	return names, nil
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
