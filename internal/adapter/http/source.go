package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	nethttp "net/http"

	"github.com/kgatilin/archai/internal/domain"
)

type sourceFilePageData struct {
	pageData
	Path  string
	Lines []sourceLine
}

type sourceLine struct {
	Number int
	Text   string
}

type sourceFileJSON struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

type saveSourceFileRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	BaseHash string `json:"baseHash"`
}

type saveSourceFileJSON struct {
	Path             string   `json:"path"`
	Content          string   `json:"content"`
	Hash             string   `json:"hash"`
	ReloadedPackages []string `json:"reloadedPackages,omitempty"`
}

func (s *Server) handleSourceFile(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	rel, content, err := s.readSourceFile(r)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	s.renderPage(w, "source.html", sourceFilePageData{
		pageData: s.basePageData(r, rel, ""),
		Path:     rel,
		Lines:    sourceLines(content),
	})
}

func (s *Server) handleSourceFileJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	switch r.Method {
	case nethttp.MethodGet:
		s.handleGetSourceFileJSON(w, r)
	case nethttp.MethodPut:
		s.handleSaveSourceFileJSON(w, r)
	default:
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetSourceFileJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	rel, content, err := s.readSourceFile(r)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(sourceFileJSON{Path: rel, Content: content, Hash: sourceHash(content)})
}

func (s *Server) handleSaveSourceFileJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	r.Body = nethttp.MaxBytesReader(w, r.Body, 4*1024*1024)
	defer r.Body.Close()

	var req saveSourceFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeSourceError(w, errSourcePath("decode source update: "+err.Error()))
		return
	}
	rel, content, reloaded, err := s.saveSourceFile(r.Context(), r, req)
	if err != nil {
		writeSourceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(saveSourceFileJSON{
		Path:             rel,
		Content:          content,
		Hash:             sourceHash(content),
		ReloadedPackages: reloaded,
	})
}

func (s *Server) readSourceFile(r *nethttp.Request) (string, string, error) {
	rel, abs, err := s.resolveSourceFile(r, r.URL.Query().Get("file"))
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", "", errSourceRead("read source: " + err.Error())
	}
	return rel, string(data), nil
}

func (s *Server) saveSourceFile(ctx context.Context, r *nethttp.Request, req saveSourceFileRequest) (string, string, []string, error) {
	rel, abs, err := s.resolveSourceFile(r, req.Path)
	if err != nil {
		return "", "", nil, err
	}
	before, err := os.ReadFile(abs)
	if err != nil {
		return "", "", nil, errSourceRead("read source: " + err.Error())
	}
	if req.BaseHash == "" {
		return "", "", nil, errSourcePath("missing baseHash")
	}
	currentHash := sourceHash(string(before))
	if req.BaseHash != currentHash {
		return rel, string(before), nil, errSourceConflict("file changed on disk")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", nil, errSourceRead("stat source: " + err.Error())
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, errSourcePath("not a regular file")
	}
	if err := os.WriteFile(abs, []byte(req.Content), info.Mode().Perm()); err != nil {
		return "", "", nil, errSourceRead("write source: " + err.Error())
	}

	reloaded := s.reloadSourceOwner(ctx, r, abs)
	return rel, req.Content, reloaded, nil
}

func (s *Server) resolveSourceFile(r *nethttp.Request, requested string) (string, string, error) {
	state := s.stateFor(r)
	if state == nil {
		return "", "", errSourceState("state unavailable")
	}
	snap := state.Snapshot()
	return resolveSourcePath(snap.Root, requested, snap.Packages)
}

func (s *Server) reloadSourceOwner(ctx context.Context, r *nethttp.Request, abs string) []string {
	if !strings.HasSuffix(abs, ".go") {
		return nil
	}
	state := s.stateFor(r)
	if state == nil {
		return nil
	}
	pkg := state.FindOwningPackage(abs)
	if pkg == "" {
		return nil
	}
	if err := state.ReloadPackage(ctx, pkg); err != nil {
		return nil
	}
	state.PublishPackageReload([]string{pkg})
	return []string{pkg}
}

func resolveSourcePath(root, requested string, packages []domain.PackageModel) (string, string, error) {
	if root == "" {
		return "", "", errSourcePath("missing project root")
	}
	if requested == "" {
		return "", "", errSourcePath("missing file")
	}
	if filepath.IsAbs(requested) {
		return "", "", errSourcePath("absolute file paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(requested))
	if clean == "." || clean == "" {
		return "", "", errSourcePath("missing file")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", errSourcePath("file escapes project root")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", errSourcePath("resolve root: " + err.Error())
	}
	full := filepath.Join(rootAbs, clean)
	rel, err := filepath.Rel(rootAbs, full)
	if err != nil {
		return "", "", errSourcePath("resolve file: " + err.Error())
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errSourcePath("file escapes project root")
	}
	if isRegularFile(full) {
		return filepath.ToSlash(rel), full, nil
	}
	if pkgRel, pkgAbs, ok := resolveSourcePathFromPackages(rootAbs, clean, packages); ok {
		return pkgRel, pkgAbs, nil
	}
	if suffixRel, suffixAbs, ok := findUniqueSourcePathBySuffix(rootAbs, clean); ok {
		return suffixRel, suffixAbs, nil
	}
	return filepath.ToSlash(rel), full, nil
}

func resolveSourcePathFromPackages(rootAbs, clean string, packages []domain.PackageModel) (string, string, bool) {
	cleanSlash := filepath.ToSlash(clean)
	fileBase := pathBase(cleanSlash)
	for _, pkg := range packages {
		if pkg.Path == "" {
			continue
		}
		pkgSlash := filepath.ToSlash(filepath.Clean(filepath.FromSlash(pkg.Path)))
		if cleanSlash != filepath.ToSlash(filepath.Join(pkgSlash, fileBase)) &&
			!strings.HasPrefix(cleanSlash, pkgSlash+"/") {
			continue
		}
		for _, source := range pkg.SourceFiles() {
			if pathBase(source) != fileBase {
				continue
			}
			candidateRel := filepath.Join(filepath.FromSlash(pkg.Path), filepath.FromSlash(source))
			candidateAbs := filepath.Join(rootAbs, candidateRel)
			if !isRegularFile(candidateAbs) {
				continue
			}
			rel, err := filepath.Rel(rootAbs, candidateAbs)
			if err != nil {
				continue
			}
			return filepath.ToSlash(rel), candidateAbs, true
		}
	}
	return "", "", false
}

func findUniqueSourcePathBySuffix(rootAbs, clean string) (string, string, bool) {
	cleanSlash := filepath.ToSlash(clean)
	fileBase := pathBase(cleanSlash)
	var foundRel string
	var foundAbs string
	ambiguous := false

	_ = filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipSourceDir(d.Name()) && path != rootAbs {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != fileBase {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash != cleanSlash && !strings.HasSuffix(relSlash, "/"+cleanSlash) {
			return nil
		}
		if foundRel != "" {
			ambiguous = true
			return filepath.SkipAll
		}
		foundRel = relSlash
		foundAbs = path
		return nil
	})

	if foundRel == "" || ambiguous {
		return "", "", false
	}
	return foundRel, foundAbs, true
}

func shouldSkipSourceDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "target":
		return true
	default:
		return false
	}
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func pathBase(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

type errSourcePath string

func (e errSourcePath) Error() string { return string(e) }

type errSourceRead string

func (e errSourceRead) Error() string { return string(e) }

type errSourceState string

func (e errSourceState) Error() string { return string(e) }

type errSourceConflict string

func (e errSourceConflict) Error() string { return string(e) }

func writeSourceError(w nethttp.ResponseWriter, err error) {
	switch err.(type) {
	case errSourcePath:
		nethttp.Error(w, err.Error(), nethttp.StatusBadRequest)
	case errSourceState:
		nethttp.Error(w, err.Error(), nethttp.StatusServiceUnavailable)
	case errSourceConflict:
		nethttp.Error(w, err.Error(), nethttp.StatusConflict)
	default:
		nethttp.Error(w, err.Error(), nethttp.StatusNotFound)
	}
}

func sourceHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sourceLines(src string) []sourceLine {
	parts := strings.Split(src, "\n")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	lines := make([]sourceLine, 0, len(parts))
	for i, text := range parts {
		lines = append(lines, sourceLine{Number: i + 1, Text: text})
	}
	if len(lines) == 0 {
		return []sourceLine{{Number: 1}}
	}
	return lines
}
