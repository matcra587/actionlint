package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/go-cmp/cmp"
)

func testRunMain(args []string) (string, string, int) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	status := run(args, stdout, stderr, io.Discard, "")
	return stdout.String(), stderr.String(), status
}

func TestOKWriteStdout(t *testing.T) {
	f := filepath.Join("testdata", "ok.md")
	stdout, stderr, status := testRunMain([]string{f, "-"})
	if status != 0 {
		t.Fatalf("status was non-zero: %d: %q", status, stderr)
	}

	b, err := os.ReadFile(filepath.Join("testdata", "ok.go"))
	if err != nil {
		panic(err)
	}
	want := string(b)

	if diff := cmp.Diff(stdout, want); diff != "" {
		t.Fatal(diff)
	}
}

func TestOKWriteFile(t *testing.T) {
	in := filepath.Join("testdata", "ok.md")
	out := filepath.Join(t.TempDir(), "generated.go")
	if err := os.WriteFile(out, []byte(strings.Repeat("old content\n", 10000)), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, status := testRunMain([]string{in, out})
	if status != 0 {
		t.Fatalf("status was non-zero: %d: %q", status, stderr)
	}

	b, err := os.ReadFile(filepath.Join("testdata", "ok.go"))
	if err != nil {
		panic(err)
	}
	want := string(b)

	if stdout != "" {
		t.Fatalf("stdout is not empty: %q", stdout)
	}

	b, err = os.ReadFile(out)
	if err != nil {
		t.Fatalf("output file %q cannot be read: %v", out, err)
	}
	have := string(b)

	if diff := cmp.Diff(want, have); diff != "" {
		t.Fatal(diff)
	}
}

func TestErrorGenerate(t *testing.T) {
	testCases := []struct {
		file string
		want string
	}{
		{"no_heading.md", "no \"Context availability\" table was found"},
		{"no_table.md", "no \"Context availability\" table was found"},
		{"broken_table.md", "expected 3 rows in table but got"},
		{"else_directive.md", "cannot strip template directives since it contains {% else %}"},
	}

	for _, tc := range testCases {
		t.Run(tc.file, func(t *testing.T) {
			f := filepath.Join("testdata", tc.file)
			stdout, stderr, status := testRunMain([]string{f, "-"})
			if status == 0 {
				t.Fatalf("status was zero: %q", stdout)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("wanted %q in stderr %q", tc.want, stderr)
			}
		})
	}
}

func TestStripAndUnescape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"foo, bar", "foo, bar"},
		{"&lt;hello&gt;", "<hello>"},
		{"aaa{% ifhoge ... %}, foo, bar{% endif %}, bbb", "aaa, foo, bar, bbb"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			have, err := stripAndUnescape(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if have != tc.want {
				t.Fatalf("wanted %q but got %q", tc.want, have)
			}
		})
	}
}

var errTestDummy = errors.New("dummy write error")

type testErrorWriter struct{}

func (w testErrorWriter) Write(b []byte) (int, error) {
	return 0, errTestDummy
}

func TestWriteError(t *testing.T) {
	f := filepath.Join("testdata", "ok.md")
	stderr := &bytes.Buffer{}
	status := run([]string{f, "-"}, testErrorWriter{}, stderr, io.Discard, "")
	if status == 0 {
		t.Fatal("status was zero")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "dummy write error") {
		t.Fatalf("write error did not occur: %q", msg)
	}
}

func TestFetchError(t *testing.T) {
	testCases := []struct {
		what string
		url  string
		want string
	}{
		{"not found", "https://raw.githubusercontent.com/matcra587/actionlint/main/this-file-does-not-exist.txt", "request was not successful"},
		{"invalid url", "foo://bar", "could not fetch"},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			stderr := &bytes.Buffer{}
			status := run([]string{"-"}, io.Discard, stderr, io.Discard, tc.url)
			if status == 0 {
				t.Fatal("status was zero")
			}
			msg := stderr.String()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("unexpected error: %v: %s", msg, tc.url)
			}
		})
	}
}

func TestCmdError(t *testing.T) {
	f := filepath.Join("testdata", "ok.md")
	dirNotExist := filepath.Join("dir", "does", "not", "exist", "out.go")
	testCases := []struct {
		what string
		args []string
		want string
	}{
		{"too many args", []string{"foo", "bar", "piyo"}, "usage:"},
		{"cannot read file", []string{"oops-this-file-does-not-exist.md", "-"}, "oops-this-file-does-not-exist.md"},
		{"cannot write file", []string{f, dirNotExist}, dirNotExist},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			stdout, stderr, status := testRunMain(tc.args)
			if status == 0 {
				t.Fatalf("status was zero: %q", stdout)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr does not contain %q: %q", tc.want, stderr)
			}
		})
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
			input := filepath.Join("testdata", "no_heading.md")
			const wantError = `no "Context availability" table was found`
			_, stderr, status := testRunMain([]string{input, output})
			if status == 0 || !strings.Contains(stderr, wantError) {
				t.Fatalf("expected parser failure, got status %d: %s", status, stderr)
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestSourceClosesResponse(t *testing.T) {
	for _, name := range []string{"success", "status error", "read error"} {
		t.Run(name, func(t *testing.T) {
			body := &trackedBody{Reader: strings.NewReader("source")}
			status := http.StatusOK
			wantError := ""
			if name == "status error" {
				status = http.StatusBadGateway
				wantError = "request was not successful"
			}
			if name == "read error" {
				body.Reader = iotest.ErrReader(errors.New("body failed"))
				wantError = "body failed"
			}
			// source uses the default transport; restore it before the next test.
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: body}, nil
			})
			got, err := source(nil, "https://example.test/contexts.md")
			if wantError == "" {
				if err != nil || string(got) != "source" {
					t.Fatalf("unexpected source: %q, %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("expected %q, got %v", wantError, err)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}
}
