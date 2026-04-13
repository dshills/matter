package workspace

import "testing"

func TestCheckHiddenPathBlocked(t *testing.T) {
	tests := []struct {
		path string
	}{
		{".env"},
		{".git/config"},
		{".ssh/id_rsa"},
		{".matter/runs/data"},
		{"subdir/.hidden"},
	}
	for _, tt := range tests {
		if err := CheckHiddenPath(tt.path, nil); err == nil {
			t.Errorf("expected hidden path %q to be blocked", tt.path)
		}
	}
}

func TestCheckHiddenPathAllowed(t *testing.T) {
	allowed := []string{".config/myapp", ".matter"}

	tests := []struct {
		path string
	}{
		{".config/myapp/settings.yaml"},
		{".config/myapp"},
		{".matter/runs/log.json"},
		{".matter"},
	}
	for _, tt := range tests {
		if err := CheckHiddenPath(tt.path, allowed); err != nil {
			t.Errorf("path %q should be allowed: %v", tt.path, err)
		}
	}
}

func TestCheckHiddenPathNonHidden(t *testing.T) {
	// Paths without hidden segments should always pass.
	paths := []string{
		"src/main.go",
		"README.md",
		"a/b/c/file.txt",
	}
	for _, p := range paths {
		if err := CheckHiddenPath(p, nil); err != nil {
			t.Errorf("non-hidden path %q should pass: %v", p, err)
		}
	}
}

func TestCheckHiddenPathPartialAllowNoMatch(t *testing.T) {
	// .git is not in allowed list, only .github is.
	allowed := []string{".github"}
	if err := CheckHiddenPath(".git/config", allowed); err == nil {
		t.Error("expected .git to be blocked when only .github is allowed")
	}
}

func TestCheckHiddenPathNestedHidden(t *testing.T) {
	// Path with hidden segment deep in the tree.
	allowed := []string{"src/.internal"}
	if err := CheckHiddenPath("src/.internal/module.go", allowed); err != nil {
		t.Errorf("allowed nested hidden path should pass: %v", err)
	}
	if err := CheckHiddenPath("src/.other/module.go", nil); err == nil {
		t.Error("non-allowed nested hidden path should be blocked")
	}
}

func TestCheckHiddenPathNoSiblingLeak(t *testing.T) {
	// Allowing .github/workflows must NOT allow .github/malicious.sh.
	allowed := []string{".github/workflows"}
	if err := CheckHiddenPath(".github/workflows/ci.yml", allowed); err != nil {
		t.Errorf("child of allowed path should pass: %v", err)
	}
	if err := CheckHiddenPath(".github/malicious.sh", allowed); err == nil {
		t.Error("sibling of allowed path should be blocked")
	}
	if err := CheckHiddenPath(".github", allowed); err == nil {
		t.Error("parent of allowed path should be blocked")
	}
}
