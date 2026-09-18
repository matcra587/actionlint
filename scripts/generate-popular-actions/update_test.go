package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestUpdateRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	initial := `[
		{"slug":"example/action","tags":["v1","v3"],"version_pattern":"v([0-9]+)"},
		{"slug":"example/action","path":"/restore","tags":["v3"],"version_pattern":"v([0-9]+)","file_ext":"yaml","skip_inputs":true,"skip_outputs":true},
		{"slug":"example/fixed","tags":["stable"]}
	]`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	g := newGen(&stdout, &stderr, io.Discard)
	listed := 0
	g.listRefs = func(_ context.Context, slug string) ([]string, error) {
		listed++
		if slug != "example/action" {
			return nil, fmt.Errorf("queried opted-out repository %q", slug)
		}
		return []string{"v10", "v2", "v4", "v5-rc1", "v4.1.0", "v3", "v11"}, nil
	}
	g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		status := http.StatusOK
		if strings.Contains(r.URL.Path, "/v11/") || strings.HasSuffix(r.URL.Path, "/v4/restore/action.yaml") {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: http.NoBody}, nil
	})}
	if status := g.run([]string{"generate", "-u", "-r", path}); status != 0 {
		t.Fatalf("update failed: %d: %s", status, &stderr)
	}
	if listed != 1 {
		t.Fatalf("listed shared repository %d times, want once", listed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var actions []*registry
	if err := json.Unmarshal(data, &actions); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(actions[0].Tags, []string{"v1", "v3", "v4", "v10"}) {
		t.Fatalf("incorrect version selection/order: %v", actions[0].Tags)
	}
	if !slices.Equal(actions[1].Tags, []string{"v3", "v10"}) {
		t.Fatalf("added missing sub-action: %v", actions[1].Tags)
	}
	if !actions[1].SkipInputs || !actions[1].SkipOutputs || actions[1].FileExt != "yaml" {
		t.Fatal("lost registry options")
	}
	if !slices.Equal(actions[2].Tags, []string{"stable"}) {
		t.Fatal("changed opted-out action")
	}
	stdout.Reset()
	if status := g.run([]string{"generate", "-u", "-r", path}); status != 0 {
		t.Fatalf("second run failed: %d: %s", status, &stderr)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) || stdout.String() != "No new release was found\n" {
		t.Fatalf("second run changed registry or reported updates: %s", &stdout)
	}
}

func TestFetchRefsConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGen(io.Discard, io.Discard, io.Discard)
		const batches = 3
		const repositories = batches * discoveryConcurrency
		actions := []*registry{}
		for i := range repositories {
			actions = append(actions, &registry{Slug: fmt.Sprintf("example/action%d", i), VersionPattern: `v([0-9]+)`})
		}
		// Sub-actions share the repository lookup; opt-outs require none.
		actions = append(actions, actions[0], &registry{Slug: "example/manual"})
		var active, peak, calls atomic.Int32
		g.listRefs = func(_ context.Context, slug string) ([]string, error) {
			calls.Add(1)
			current := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); current > old; old = peak.Load() {
				if peak.CompareAndSwap(old, current) {
					break
				}
			}
			time.Sleep(time.Second)
			return []string{slug}, nil
		}
		start := time.Now()
		refs, err := g.fetchRefs(t.Context(), actions)
		if err != nil {
			t.Fatal(err)
		}
		if got := peak.Load(); got <= 1 || got > discoveryConcurrency {
			t.Fatalf("peak concurrent queries = %d, want 2..%d", got, discoveryConcurrency)
		}
		if calls.Load() != repositories || len(refs) != repositories {
			t.Fatalf("expected %d unique repositories, got %d calls and %d results", repositories, calls.Load(), len(refs))
		}
		for slug, values := range refs {
			if !slices.Equal(values, []string{slug}) {
				t.Fatalf("mixed up repository results for %s: %v", slug, values)
			}
		}
		if elapsed := time.Since(start); elapsed != batches*time.Second {
			t.Fatalf("expected %d parallel batches, took %s", batches, elapsed)
		}
	})
}

func TestFetchRefsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGen(io.Discard, io.Discard, io.Discard)
		const batches = 3
		const repositories = batches * discoveryConcurrency
		actions := []*registry{}
		for i := range repositories {
			actions = append(actions, &registry{Slug: fmt.Sprintf("example/action%d", i), VersionPattern: `v([0-9]+)`})
		}
		failure := errors.New("ref query failed")
		var active, calls atomic.Int32
		g.listRefs = func(ctx context.Context, slug string) ([]string, error) {
			calls.Add(1)
			active.Add(1)
			defer active.Add(-1)
			if slug == "example/action0" {
				time.Sleep(time.Second)
				return nil, failure
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		refs, err := g.fetchRefs(t.Context(), actions)
		if !errors.Is(err, failure) || refs != nil {
			t.Fatalf("expected original failure without partial results, got %v, %v", refs, err)
		}
		if active.Load() != 0 {
			t.Fatal("returned before in-flight queries stopped")
		}
		if calls.Load() != discoveryConcurrency {
			t.Fatalf("expected only the first batch to query refs, got %d", calls.Load())
		}
	})
}

type closeErrorBody struct{ io.Reader }

func (closeErrorBody) Close() error {
	return errors.New("close failed")
}

func TestUpdateFailurePreservesRegistry(t *testing.T) {
	for _, failure := range []string{"refs", "http", "transport", "close"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.json")
			initial := `[{"slug":"example/action","tags":["v1"],"version_pattern":"v([0-9]+)"}]`
			if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			g := newGen(io.Discard, &stderr, io.Discard)
			g.listRefs = func(context.Context, string) ([]string, error) {
				if failure == "refs" {
					return nil, errors.New("ref listing failed")
				}
				return []string{"v2", "v3"}, nil
			}
			g.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				// Discover v2 successfully before failing on v3.
				status := http.StatusOK
				if strings.Contains(r.URL.Path, "/v3/") {
					if failure == "transport" {
						return nil, errors.New("connection failed")
					}
					if failure == "close" {
						return &http.Response{StatusCode: http.StatusNotFound, Body: closeErrorBody{}}, nil
					}
					status = http.StatusTooManyRequests
				}
				return &http.Response{StatusCode: status, Body: http.NoBody}, nil
			})}
			if status := g.run([]string{"generate", "-u", "-r", path}); status != 1 {
				t.Fatalf("failure returned %d, want 1", status)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != initial {
				t.Fatal("failed discovery changed registry")
			}
			if stderr.Len() == 0 {
				t.Fatal("failure has no diagnostic")
			}
		})
	}
}

func TestDiscoveryVersionPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, current, next, rejected string
	}{
		{`v([0-9]+)`, "v9", "v10", "v11-rc1"},
		{`v([0-9]+)\.x`, "v3.x", "v4.x", "v5.0.0"},
		{`release/v([0-9]+)`, "release/v1", "release/v2", "v3"},
		{`v([0-9]+\.[0-9]+\.[0-9]+)`, "v4.3.6", "v4.3.10", "v4.4.0-beta.1"},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			g := newGen(io.Discard, io.Discard, io.Discard)
			g.rawRegistry = fmt.Appendf(nil,
				`[{"slug":"example/action","tags":[%q],"version_pattern":%q}]`, tc.current, tc.pattern)
			g.listRefs = func(context.Context, string) ([]string, error) {
				return []string{tc.current, tc.next, tc.rejected}, nil
			}
			g.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			})}
			_, added, err := g.discoverVersions(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(added, []string{"example/action@" + tc.next}) {
				t.Fatalf("incorrect candidates: %v", added)
			}
		})
	}
}
