package output

import (
	"os"
	"strings"
	"testing"
)

func TestSetOutput_File(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "github-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	t.Setenv("GITHUB_OUTPUT", tmpFile.Name())

	if err := SetOutput("key", "value"); err != nil {
		t.Fatalf("SetOutput() error = %v", err)
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), "key=value") {
		t.Errorf("output file contains %q, want key=value", string(data))
	}
}

func TestSetOutput_Multiline(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "github-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	t.Setenv("GITHUB_OUTPUT", tmpFile.Name())

	if err := SetOutput("body", "line1\nline2"); err != nil {
		t.Fatalf("SetOutput() error = %v", err)
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "body<<EOF") {
		t.Errorf("output file contains %q, want heredoc format", content)
	}
}

// The log helpers emit GitHub's annotation syntax. A warning printed as plain
// text still appears in the log but never reaches the run's summary page, which
// is where a missing attempt has to be visible.
func TestLogHelpersEmitAnnotationSyntax(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string)
		want string
	}{
		{"info", LogInfo, "hello\n"},
		{"warning", LogWarning, "::warning::hello\n"},
		{"error", LogError, "::error::hello\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			orig := os.Stdout
			os.Stdout = w
			tc.fn("hello")
			w.Close()
			os.Stdout = orig

			buf := make([]byte, 128)
			n, _ := r.Read(buf)
			if got := string(buf[:n]); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// A value containing a newline must use the heredoc form. Written as key=value
// it terminates at the first newline and the rest is parsed as further keys —
// which is exactly the shape of the rendered comparison table.
func TestMultilineOutputUsesTheHeredocForm(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Setenv("GITHUB_OUTPUT", f.Name())

	if err := SetOutput("comparison", "| a |\n| b |"); err != nil {
		t.Fatalf("SetOutput: %v", err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "comparison<<EOF") || !strings.HasSuffix(strings.TrimSpace(got), "EOF") {
		t.Fatalf("multiline output was not written as a heredoc:\n%s", got)
	}
}
