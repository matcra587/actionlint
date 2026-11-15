package actionlint_test

import (
	"strings"
	"testing"

	"github.com/matcra587/actionlint"
)

func TestParseCacheMode(t *testing.T) {
	for _, mode := range []string{"read", "write", "write-only", "none"} {
		for _, scope := range []string{"workflow", "job", "reusable workflow call"} {
			t.Run(mode+"/"+scope, func(t *testing.T) {
				source := "on: push\n"
				if scope == "workflow" {
					source += "cache-mode: " + mode + "\n"
				}
				source += "jobs:\n  check:\n"
				if scope != "workflow" {
					source += "    cache-mode: " + mode + "\n"
				}
				if scope == "reusable workflow call" {
					source += "    uses: owner/repo/.github/workflows/reusable.yml@v1\n"
				} else {
					source += "    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				}
				workflow, errs := actionlint.Parse([]byte(source))
				if len(errs) != 0 {
					t.Fatalf("valid cache mode rejected: %v", errs)
				}
				got := workflow.CacheMode
				if scope != "workflow" {
					got = workflow.Jobs["check"].CacheMode
				}
				if got == nil || got.Value != mode {
					t.Fatalf("cache mode was not preserved in syntax tree: %v", got)
				}
				if scope == "reusable workflow call" && workflow.Jobs["check"].WorkflowCall == nil {
					t.Fatal("cache mode prevented parsing the reusable workflow call")
				}
			})
		}
	}
}

func TestParseCacheModeInvalid(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{"unknown mode", "invalid", "is invalid for cache-mode"},
		{"case-sensitive mode", "READ", "is invalid for cache-mode"},
		{"empty mode", "''", "string should not be empty"},
		{"mapping mode", "{}", "expected scalar node for string value"},
		{"sequence mode", "[read]", "expected scalar node for string value"},
	} {
		for _, scope := range []string{"workflow", "job"} {
			t.Run(tc.name+"/"+scope, func(t *testing.T) {
				source := "on: push\n"
				if scope == "workflow" {
					source += "cache-mode: " + tc.value + "\n"
				}
				source += "jobs:\n  check:\n"
				if scope == "job" {
					source += "    cache-mode: " + tc.value + "\n"
				}
				source += "    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
				_, errs := actionlint.Parse([]byte(source))
				if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
					t.Fatalf("wanted one %q error, got %v", tc.want, errs)
				}
			})
		}
	}
}

func concurrencyWorkflow(scope, config string) string {
	if scope == "workflow" {
		return "on: push\nconcurrency:\n  group: test\n  " + strings.ReplaceAll(config, "\n", "\n  ") +
			"\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	}
	return "on: push\njobs:\n  check:\n    concurrency:\n      group: test\n      " +
		strings.ReplaceAll(config, "\n", "\n      ") +
		"\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
}

func TestParseConcurrencyQueue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   string
	}{
		{"single", "queue: single\ncancel-in-progress: true", ""},
		{"max", "queue: max", ""},
		{"max without cancellation", "queue: max\ncancel-in-progress: false", ""},
		{"dynamic cancellation", "queue: max\ncancel-in-progress: ${{ github.ref == 'refs/heads/main' }}", ""},
		{"dynamic queue", "queue: ${{ vars.QUEUE }}\ncancel-in-progress: true", ""},
		{"unknown mode", "queue: invalid", "is invalid for queue in concurrency"},
		{"case-sensitive mode", "queue: MAX", "is invalid for queue in concurrency"},
		{"empty mode", "queue: ''", "string should not be empty"},
		{"sequence mode", "queue: [max]", "expected scalar node for string value"},
		{"mapping mode", "queue: {}", "expected scalar node for string value"},
		{"conflicting cancellation", "queue: max\ncancel-in-progress: true", "cannot be combined"},
	} {
		for _, scope := range []string{"workflow", "job"} {
			t.Run(tc.name+"/"+scope, func(t *testing.T) {
				workflow, errs := actionlint.Parse([]byte(concurrencyWorkflow(scope, tc.config)))
				if tc.want != "" {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
						t.Fatalf("wanted one %q error, got %v", tc.want, errs)
					}
					return
				}
				if len(errs) != 0 {
					t.Fatalf("valid concurrency configuration rejected: %v", errs)
				}
				concurrency := workflow.Concurrency
				if scope == "job" {
					concurrency = workflow.Jobs["check"].Concurrency
				}
				if concurrency == nil || concurrency.Queue == nil || concurrency.Queue.Value == "" {
					t.Fatal("queue was not preserved in syntax tree")
				}
			})
		}
	}
}

func TestConcurrencyQueueExpressions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope string
		queue string
		want  string
	}{
		{"workflow context", "workflow", "${{ vars.QUEUE }}", ""},
		{"job context", "job", "${{ github.ref == 'refs/heads/main' && 'max' || 'single' }}", ""},
		{"unknown context property", "workflow", "${{ github.unknown_property }}", "property \"unknown_property\" is not defined"},
		{"unavailable workflow context", "workflow", "${{ secrets.QUEUE }}", "context \"secrets\" is not allowed here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow, errs := actionlint.Parse([]byte(concurrencyWorkflow(tc.scope, "queue: "+tc.queue)))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := actionlint.NewRuleExpression(nil, nil)
			visitor := actionlint.NewVisitor()
			visitor.AddPass(rule)
			if err := visitor.Visit(workflow); err != nil {
				t.Fatal(err)
			}
			errs = rule.Errs()
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("valid queue expression rejected: %v", errs)
				}
			} else if len(errs) != 1 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("wanted one %q error, got %v", tc.want, errs)
			}
		})
	}
}
