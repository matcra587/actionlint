package actionlint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseNaNTimeout(t *testing.T) {
	for _, value := range []string{"!!float NaN", "!!float nan", "!!float NAN", ".nan"} {
		for _, scope := range []string{"job", "step"} {
			t.Run(scope+"/"+value, func(t *testing.T) {
				defer func() {
					if panicValue := recover(); panicValue != nil {
						t.Fatalf("invalid timeout panicked instead of returning a diagnostic: %v", panicValue)
					}
				}()
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
				if scope == "job" {
					source += "    timeout-minutes: " + value + "\n    steps:\n      - run: echo ok\n"
				} else {
					source += "    steps:\n      - run: echo ok\n        timeout-minutes: " + value + "\n"
				}
				_, errs := Parse([]byte(source))
				if len(errs) != 1 || !strings.Contains(errs[0].Message, "invalid float value") {
					t.Fatalf("expected one invalid timeout diagnostic, got %v", errs)
				}
			})
		}
	}
}

func TestParseContainerPortsAndVolumes(t *testing.T) {
	for _, scope := range []string{"job", "service"} {
		for _, order := range []string{"ports first", "volumes first"} {
			t.Run(scope+"/"+order, func(t *testing.T) {
				ports := "ports: ['8080:80', '9090:90']\n"
				volumes := "volumes: ['cache:/cache', '/tmp/data:/data']\n"
				settings := ports + volumes
				if order == "volumes first" {
					settings = volumes + ports
				}
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
				if scope == "job" {
					source += "    container:\n      image: busybox\n      " +
						strings.ReplaceAll(strings.TrimSuffix(settings, "\n"), "\n", "\n      ") + "\n"
				} else {
					source += "    services:\n      database:\n        image: busybox\n        " +
						strings.ReplaceAll(strings.TrimSuffix(settings, "\n"), "\n", "\n        ") + "\n"
				}
				source += "    steps:\n      - run: echo ok\n"
				workflow, errs := Parse([]byte(source))
				if len(errs) != 0 {
					t.Fatal(errs)
				}
				container := workflow.Jobs["test"].Container
				if scope == "service" {
					container = workflow.Jobs["test"].Services.Value["database"].Container
				}
				values := func(entries []*String) []string {
					result := make([]string, len(entries))
					for i, entry := range entries {
						result[i] = entry.Value
					}
					return result
				}
				if got := values(container.Ports); !slices.Equal(got, []string{"8080:80", "9090:90"}) {
					t.Errorf("ports were overwritten: %v", got)
				}
				if got := values(container.Volumes); !slices.Equal(got, []string{"cache:/cache", "/tmp/data:/data"}) {
					t.Errorf("volumes were not preserved separately: %v", got)
				}
			})
		}
	}
}

func BenchmarkParseWorkflow(b *testing.B) {
	type bench struct {
		name  string
		input []byte
	}

	loadBench := func(name string) bench {
		i, err := os.ReadFile(filepath.Join("testdata", "bench", name+".yaml"))
		if err != nil {
			b.Fatal(err)
		}
		return bench{name, i}
	}

	for _, bc := range []bench{
		loadBench("minimal"),
		loadBench("small"),
		loadBench("large"),
	} {
		b.Run(bc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, errs := Parse(bc.input); len(errs) > 0 {
					b.Fatal(errs)
				}
			}
		})
	}
}

func BenchmarkParseTestData(b *testing.B) {
	type bench struct {
		name   string
		inputs [][]byte
	}

	loadBench := func(name string) bench {
		inputs := [][]byte{}
		_, fs, err := testFindAllWorkflowsInDir(name)
		if err != nil {
			b.Fatal(err)
		}
		for _, f := range fs {
			bs, err := os.ReadFile(f)
			if err != nil {
				b.Fatal(err)
			}
			inputs = append(inputs, bs)
		}
		return bench{name, inputs}
	}

	for _, bc := range []bench{
		loadBench("examples"),
		loadBench("ok"),
		loadBench("err"),
	} {
		b.Run(bc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				for _, in := range bc.inputs {
					// Note: Some workflows may cause parse error
					Parse(in)
				}
			}
		})
	}
}
