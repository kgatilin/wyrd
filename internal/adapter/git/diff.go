// Package git reads the working state of a git repository.
//
// It is an inbound adapter: it shells out to the git CLI and turns its
// output into plain data. Nothing here knows about the wyrd model —
// the review UI's file-level diff is text, not architecture.
//
// The diff produced by Diff answers "what did this branch change",
// which means three-dot semantics against the review base: the range
// starts at merge-base(base, HEAD) and ends at the *working tree*, so
// uncommitted work an agent has not committed yet still shows up.
// Untracked (non-ignored) files are appended as synthetic additions,
// because a review that silently hides new files is a trap.
package git

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// maxPatchBytes caps a single file's unified patch. Generated files and
// vendored blobs produce multi-megabyte hunks that no reviewer reads and
// that would dominate the JSON payload; past the cap the patch is cut on
// a line boundary and marked Truncated.
const maxPatchBytes = 256 * 1024

// emptyTreeRev is git's well-known hash of the empty tree. It stands in
// for the base revision in a repository whose HEAD is unborn (initialized
// but never committed), where every path is an addition.
const emptyTreeRev = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// FileStat is one changed file with its stats and its unified patch.
type FileStat struct {
	// Path is the post-change path (the "b" side of the diff header).
	Path string
	// OldPath is the pre-change path, set only for renames and copies.
	OldPath string
	// Status is the git name-status code: A, M, D, R100, C75, ...
	Status string
	// Insertions and Deletions are numstat counts (0 for binary files).
	Insertions int
	Deletions  int
	// Binary marks a file git refused to diff textually.
	Binary bool
	// Untracked marks a file that exists only in the working tree.
	Untracked bool
	// Patch is the unified diff for this file alone, including its
	// `diff --git` header. Empty for mode-only changes.
	Patch string
	// Truncated reports that Patch was cut at maxPatchBytes.
	Truncated bool
}

// Result is the full diff of a worktree against the review base.
type Result struct {
	// Branch is the checked-out branch ("HEAD" when detached).
	Branch string
	// BaseRef is the ref that was requested (e.g. "main").
	BaseRef string
	// BaseRev is the revision the diff actually starts from: the merge
	// base with BaseRef, or "HEAD" when no base could be resolved (in
	// which case the result holds only uncommitted work).
	BaseRev string
	// Files is sorted by path.
	Files []FileStat
}

// Diff computes the working-tree diff of repoPath against baseRef.
//
// baseRef is resolved leniently: the ref itself, then origin/<ref>, and
// finally HEAD when neither exists — a fresh repo with no "main" still
// yields a usable (uncommitted-only) diff instead of an error.
func Diff(repoPath, baseRef string) (Result, error) {
	if _, err := run(repoPath, "rev-parse", "--git-dir"); err != nil {
		return Result{}, fmt.Errorf("not a git repository: %w", err)
	}

	res := Result{
		Branch:  currentBranch(repoPath),
		BaseRef: baseRef,
		BaseRev: resolveBaseRev(repoPath, baseRef),
	}

	// One `git diff` call for the patches, one numstat, one name-status.
	// Running `git diff -- <path>` per file instead costs a subprocess per
	// changed file, which on a 40-file branch is a visible stall.
	numstatOut, err := run(repoPath, "diff", "--numstat", "-z", "--find-renames", res.BaseRev)
	if err != nil {
		return Result{}, fmt.Errorf("git diff --numstat: %w", err)
	}
	nameStatusOut, err := run(repoPath, "diff", "--name-status", "-z", "--find-renames", res.BaseRev)
	if err != nil {
		return Result{}, fmt.Errorf("git diff --name-status: %w", err)
	}
	fullOut, err := run(repoPath, "diff", "--no-color", "--find-renames", res.BaseRev)
	if err != nil {
		return Result{}, fmt.Errorf("git diff: %w", err)
	}

	patches := splitPatchByFile(fullOut)
	statuses := map[string]nameStatusEntry{}
	for _, e := range parseNameStatusZ(nameStatusOut) {
		statuses[e.path] = e
	}

	for _, entry := range parseNumstatZ(numstatOut) {
		st := statuses[entry.path]
		status := st.status
		if status == "" {
			status = "M"
		}
		f := FileStat{
			Path:       entry.path,
			OldPath:    st.oldPath,
			Status:     status,
			Insertions: entry.insertions,
			Deletions:  entry.deletions,
			Binary:     entry.binary,
		}
		f.Patch, f.Truncated = capPatch(patches[entry.path])
		res.Files = append(res.Files, f)
	}

	res.Files = append(res.Files, untrackedFiles(repoPath)...)
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, nil
}

// currentBranch names the checked-out branch. `branch --show-current`
// (unlike `rev-parse --abbrev-ref HEAD`) also works in a repository with
// an unborn HEAD; it returns empty when HEAD is detached, in which case
// the short revision is the useful label.
func currentBranch(repoPath string) string {
	if out, err := run(repoPath, "branch", "--show-current"); err == nil {
		if branch := strings.TrimSpace(out); branch != "" {
			return branch
		}
	}
	if out, err := run(repoPath, "rev-parse", "--short", "HEAD"); err == nil {
		if rev := strings.TrimSpace(out); rev != "" {
			return rev
		}
	}
	return "HEAD"
}

// MergeBase returns the commit where the current branch diverged from the
// review base — the ref itself or origin/<ref>, whichever resolves first.
//
// This is the single definition of "what this branch is reviewed against",
// shared by the textual diff and by the architecture model that is diffed
// beside it. Comparing against the base's *tip* instead would report every
// change that landed on the base after the branch point as this branch's
// doing, in reverse.
func MergeBase(repoPath, baseRef string) (string, bool) {
	cand, ok := resolveBaseCandidate(repoPath, baseRef)
	if !ok {
		return "", false
	}
	out, err := run(repoPath, "merge-base", cand, "HEAD")
	if err != nil {
		return "", false
	}
	rev := strings.TrimSpace(out)
	return rev, rev != ""
}

// HeadRev returns the commit HEAD points at, or "" when HEAD is unborn.
func HeadRev(repoPath string) string {
	out, err := run(repoPath, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// IsClean reports whether the working tree has no modifications and no
// untracked files — i.e. whether it still matches its HEAD commit. A caller
// that wants to reuse an already-parsed worktree as a stand-in for a commit
// needs this: a dirty checkout is not that commit.
func IsClean(repoPath string) bool {
	out, err := run(repoPath, "status", "--porcelain", "--untracked-files=all")
	return err == nil && strings.TrimSpace(out) == ""
}

// resolveBaseCandidate returns the first of {ref, origin/ref} that names a
// commit in this repository.
func resolveBaseCandidate(repoPath, baseRef string) (string, bool) {
	for _, cand := range baseCandidates(baseRef) {
		if _, err := run(repoPath, "rev-parse", "--verify", "--quiet", cand+"^{commit}"); err == nil {
			return cand, true
		}
	}
	return "", false
}

// resolveBaseRev turns a review base ref into the revision the diff runs
// from: the merge base, falling back to the ref itself when histories are
// unrelated, then to HEAD (or the empty tree) when the ref does not exist.
func resolveBaseRev(repoPath, baseRef string) string {
	if rev, ok := MergeBase(repoPath, baseRef); ok {
		return rev
	}
	if cand, ok := resolveBaseCandidate(repoPath, baseRef); ok {
		return cand
	}
	if HeadRev(repoPath) == "" {
		return emptyTreeRev // unborn HEAD: everything is an addition
	}
	return "HEAD"
}

func baseCandidates(baseRef string) []string {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		return nil
	}
	return []string{baseRef, "origin/" + baseRef}
}

// untrackedFiles synthesizes a FileStat per untracked, non-ignored file.
// `git diff --no-index /dev/null <path>` produces a real unified patch for
// it, and exits 1 because "there are differences" — expected, not an error.
func untrackedFiles(repoPath string) []FileStat {
	out, err := run(repoPath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil
	}
	var files []FileStat
	for _, path := range strings.Split(out, "\x00") {
		if path == "" {
			continue
		}
		patch, err := runAllowingDiffExit(repoPath, "diff", "--no-color", "--no-index", "--", "/dev/null", path)
		if err != nil {
			continue
		}
		f := FileStat{Path: path, Status: "A", Untracked: true}
		f.Insertions, f.Binary = summarizePatch(patch)
		f.Patch, f.Truncated = capPatch(patch)
		files = append(files, f)
	}
	return files
}

// summarizePatch derives the stats git does not report for an untracked
// file. Both checks are line-anchored on purpose: a patch's own content
// lines carry a "+" prefix, so a file that merely *mentions* "Binary
// files ... differ" is not mistaken for a binary blob.
func summarizePatch(patch string) (insertions int, binary bool) {
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"):
			// The header's target path, not content.
		case strings.HasPrefix(line, "+"):
			insertions++
		case strings.HasPrefix(line, "Binary files "):
			binary = true
		}
	}
	return insertions, binary
}

// capPatch trims a patch to maxPatchBytes on a line boundary.
func capPatch(patch string) (string, bool) {
	if len(patch) <= maxPatchBytes {
		return patch, false
	}
	cut := patch[:maxPatchBytes]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut, true
}

type numstatEntry struct {
	path       string
	insertions int
	deletions  int
	binary     bool
}

type nameStatusEntry struct {
	status  string
	path    string
	oldPath string
}

// parseNumstatZ parses `git diff --numstat -z`. Records are
// `add<TAB>del<TAB>path<NUL>`, except renames/copies which emit
// `add<TAB>del<TAB><NUL>old<NUL>new<NUL>` — the path field is empty and
// the two paths follow as separate NUL-delimited fields.
func parseNumstatZ(out string) []numstatEntry {
	if out == "" {
		return nil
	}
	fields := strings.Split(out, "\x00")
	entries := make([]numstatEntry, 0, len(fields))
	for i := 0; i < len(fields); {
		record := fields[i]
		i++
		if record == "" {
			continue
		}
		parts := strings.SplitN(record, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		path := parts[2]
		if path == "" {
			if i+1 >= len(fields) {
				break
			}
			path = fields[i+1] // the "new" side of a rename
			i += 2
		}
		ins, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		entries = append(entries, numstatEntry{
			path:       path,
			insertions: ins,
			deletions:  del,
			binary:     parts[0] == "-" && parts[1] == "-",
		})
	}
	return entries
}

// parseNameStatusZ parses `git diff --name-status -z`. Rename and copy
// records carry two paths (old, new); every other status carries one.
func parseNameStatusZ(out string) []nameStatusEntry {
	if out == "" {
		return nil
	}
	fields := strings.Split(out, "\x00")
	entries := make([]nameStatusEntry, 0, len(fields)/2)
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			continue
		}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i+1 >= len(fields) {
				break
			}
			entries = append(entries, nameStatusEntry{status: status, oldPath: fields[i], path: fields[i+1]})
			i += 2
			continue
		}
		if i >= len(fields) {
			break
		}
		entries = append(entries, nameStatusEntry{status: status, path: fields[i]})
		i++
	}
	return entries
}

// splitPatchByFile partitions one `git diff` output into a
// {path → per-file patch} map, keyed by the "b" side so renames resolve
// to the post-rename path (which is what numstat reports).
func splitPatchByFile(full string) map[string]string {
	if full == "" {
		return nil
	}
	out := map[string]string{}
	var curPath string
	var buf strings.Builder
	flush := func() {
		if curPath != "" && buf.Len() > 0 {
			out[curPath] = strings.TrimRight(buf.String(), "\n")
		}
		buf.Reset()
	}
	for _, line := range strings.Split(full, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			curPath = parsePatchHeaderPath(line)
		}
		if curPath != "" {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()
	return out
}

// parsePatchHeaderPath extracts the "b" path from a `diff --git a/<old>
// b/<new>` header. git C-quotes paths containing spaces or specials, so
// the b-side can be preceded by a bare space or by space+quote.
func parsePatchHeaderPath(header string) string {
	rest := strings.TrimPrefix(header, "diff --git ")
	if i := strings.LastIndex(rest, ` "b/`); i >= 0 {
		return strings.TrimSuffix(rest[i+len(` "b/`):], `"`)
	}
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+len(" b/"):]
	}
	return ""
}

func run(repoPath string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repoPath}, args...)...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

// runAllowingDiffExit runs a git diff variant whose exit code 1 means
// "differences found", not failure.
func runAllowingDiffExit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 1 {
			return "", err
		}
	}
	return string(out), nil
}
