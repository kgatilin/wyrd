package worktree

import "testing"

func TestParseWorktreeList_Porcelain(t *testing.T) {
	input := "worktree /home/u/proj\nHEAD abcdef1\nbranch refs/heads/main\n\n" +
		"worktree /home/u/proj-feat\nHEAD 123456a\nbranch refs/heads/feat/x\n\n" +
		"worktree /home/u/proj-detached\nHEAD deadbee\ndetached\n\n"

	got, err := ParseWorktreeList(input)
	if err != nil {
		t.Fatalf("ParseWorktreeList: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(got), got)
	}
	if got[0].Path != "/home/u/proj" || got[0].Branch != "main" || got[0].Name != "proj" {
		t.Errorf("entry[0] = %+v", got[0])
	}
	if got[1].Branch != "feat/x" || got[1].Name != "proj-feat" {
		t.Errorf("entry[1] = %+v", got[1])
	}
	if got[2].Branch != "" || got[2].Head != "deadbee" {
		t.Errorf("entry[2] = %+v", got[2])
	}
}

func TestParseWorktreeList_Bare(t *testing.T) {
	input := "worktree /srv/repo.git\nbare\n\n" +
		"worktree /srv/repo-work\nHEAD aaa111\nbranch refs/heads/main\n\n"

	got, err := ParseWorktreeList(input)
	if err != nil {
		t.Fatalf("ParseWorktreeList: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if !got[0].Bare {
		t.Errorf("entry[0] should be bare: %+v", got[0])
	}
	if got[1].Bare {
		t.Errorf("entry[1] should not be bare: %+v", got[1])
	}
}

func TestParseWorktreeList_TrailingNoBlank(t *testing.T) {
	// Some git versions may omit the trailing blank line; the parser
	// should still emit the final record.
	input := "worktree /home/u/proj\nHEAD abc\nbranch refs/heads/main"

	got, err := ParseWorktreeList(input)
	if err != nil {
		t.Fatalf("ParseWorktreeList: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Branch != "main" {
		t.Errorf("branch = %q", got[0].Branch)
	}
}
