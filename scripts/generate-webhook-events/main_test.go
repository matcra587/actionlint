package main

import (
	"errors"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestWriteStdoutOK(t *testing.T) {
	in := filepath.Join("testdata", "ok.html")
	stdout := &strings.Builder{}
	if err := run([]string{in, "-"}, stdout, io.Discard, ""); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join("testdata", "ok.go"))
	if err != nil {
		t.Fatal(err)
	}

	want := strings.ReplaceAll(string(b), "\r\n", "\n")
	have := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	if diff := cmp.Diff(want, have); diff != "" {
		t.Fatal(diff)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "", have, parser.AllErrors); err != nil {
		t.Fatalf("Input is not valid as Go. Error: %s\nSource: %s", err, have)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("dummy write error")
}

func TestWriteError(t *testing.T) {
	in := filepath.Join("testdata", "ok.html")
	err := run([]string{in, "-"}, errWriter{}, io.Discard, "")
	if err == nil {
		t.Fatal("error did not occur")
	}
	if !strings.Contains(err.Error(), "could not write output") {
		t.Fatalf("unexpected error: %q", err)
	}
	if !strings.Contains(err.Error(), "dummy write error") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestInvalidCommandArgs(t *testing.T) {
	err := run([]string{"a", "b", "c"}, io.Discard, io.Discard, "")
	if err == nil {
		t.Fatal("error did not occur")
	}
	if !strings.Contains(err.Error(), "usage: generate-webhook-events [[srcfile] dstfile]") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestParseError(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"no_article_body.html", "article body was not found"},
		{"no_markdown_body.html", "markdown body was not found"},
		{"no_heading.html", `"About events that trigger workflows" heading was missing`},
		{"no_tables.html", "no webhook table was found"},
		{"mismatched_aria_labelledby.html", `could not parse webhook types table for "check_run": expected table[aria-labelledby] to be "check_run", got "check_suite"`},
		{"no_thead.html", `could not parse webhook types table for "check_run": missing thead element`},
		{"no_header_row.html", `could not parse webhook types table for "check_run": missing header row in thead`},
		{"too_few_headers.html", `could not parse webhook types table for "check_run": expected at least 2 header columns, got 1`},
		{"non_webhook_table.html", `could not parse webhook types table for "check_run": expected first header to be "Webhook event payload", got "Operator"`},
		{"bad_second_header.html", `could not parse webhook types table for "check_run": expected second header to be "Activity types", got "Description"`},
		{"no_tbody.html", `could not parse webhook types table for "check_run": missing tbody element`},
		{"no_table_row.html", `could not parse webhook types table for "check_run": missing first data row in tbody`},
		{"too_few_columns.html", `could not parse webhook types table for "check_run": expected at least 2 data columns, got 1`},
		{"bad_activity_types.html", `could not parse webhook types table for "check_run": unexpected activity types cell text "Surprise"; expected code elements, "Not applicable", or "Custom"`},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			stdout := &strings.Builder{}
			err := run([]string{filepath.Join("testdata", tc.file), "-"}, stdout, io.Discard, "")
			if err == nil {
				t.Fatal("error did not occur")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q in error %q", tc.want, err)
			}
		})
	}
}

func TestFetchURL(t *testing.T) {
	b, err := fetch("https://github.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("Fetched source is empty")
	}
}

func TestFailedGenerationPreservesOutput(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "absent"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "generated.go")
			const original = "// previous generated output\n"
			if existing {
				if err := os.WriteFile(output, []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			input := filepath.Join("testdata", "no_heading.html")
			const wantError = `heading was missing`
			err := run([]string{input, output}, io.Discard, io.Discard, "")
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("expected parser failure, got %v", err)
			}
			contents, err := os.ReadFile(output)
			if !existing {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed generation created output: %q, %v", contents, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != original {
				t.Fatalf("failed generation changed output: %q", contents)
			}
		})
	}
}

func TestWriteFile(t *testing.T) {
	for _, name := range []string{"new", "existing", "invalid destination"} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "generated.go")
			switch name {
			case "existing":
				if err := os.WriteFile(output, []byte(strings.Repeat("old content\n", 10000)), 0o644); err != nil {
					t.Fatal(err)
				}
			case "invalid destination":
				if err := os.Mkdir(output, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var stdout strings.Builder
			err := run([]string{filepath.Join("testdata", "ok.html"), output}, &stdout, io.Discard, "")
			if name == "invalid destination" {
				if err == nil || !strings.Contains(err.Error(), "could not write output") {
					t.Fatalf("expected write failure, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			have, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "ok.go"))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(strings.ReplaceAll(string(want), "\r\n", "\n"), string(have)); diff != "" {
				t.Fatal(diff)
			}
			if stdout.Len() != 0 {
				t.Fatalf("file output also wrote to stdout: %q", stdout.String())
			}
		})
	}
}
