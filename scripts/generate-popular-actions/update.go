package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

const discoveryConcurrency = 8

// githubRefs uses Git's public ref advertisement rather than the rate-limited REST API.
// Branch aliases such as release/v1 are supported alongside release tags.
func githubRefs(ctx context.Context, slug string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--refs", "--tags", "--heads", "https://github.com/"+slug+".git")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list refs for %s: %w", slug, err)
	}
	refs := []string{}
	for line := range strings.SplitSeq(string(output), "\n") {
		_, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		ref = strings.TrimPrefix(strings.TrimPrefix(ref, "refs/tags/"), "refs/heads/")
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return slices.Compact(refs), nil
}

// version_pattern has one capture group containing the numeric version.
// Anchoring here prevents prereleases or similarly named branches from matching.
func versionPattern(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	if err != nil {
		return nil, fmt.Errorf("parse version pattern %q: %w", pattern, err)
	}
	if re.NumSubexp() != 1 {
		return nil, fmt.Errorf("version pattern %q must capture exactly one numeric version", pattern)
	}
	return re, nil
}

func refVersion(re *regexp.Regexp, ref string) ([]uint64, error) {
	m := re.FindStringSubmatch(ref)
	if m == nil {
		return nil, nil
	}
	version := []uint64{}
	for part := range strings.SplitSeq(m[1], ".") {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse version in ref %q: %w", ref, err)
		}
		version = append(version, n)
	}
	return version, nil
}

// Fetch each repository once, overlapping network waits without spawning an
// unbounded number of Git processes. Results are consumed in registry order.
func (g *gen) fetchRefs(ctx context.Context, actions []*registry) (map[string][]string, error) {
	slugs := []string{}
	seen := map[string]bool{}
	for _, action := range actions {
		if action.VersionPattern != "" && !seen[action.Slug] {
			slugs = append(slugs, action.Slug)
			seen[action.Slug] = true
		}
	}
	refs := make([][]string, len(slugs))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(discoveryConcurrency)
	for i, slug := range slugs {
		group.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			g.log.Println("Discovering versions for", slug)
			var err error
			refs[i], err = g.listRefs(ctx, slug)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	byRepo := make(map[string][]string, len(slugs))
	for i, slug := range slugs {
		byRepo[slug] = refs[i]
	}
	return byRepo, nil
}

func (g *gen) metadataExists(ctx context.Context, url string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, fmt.Errorf("request metadata for %s: %w", url, err)
	}
	res, err := g.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check metadata for %s: %w", url, err)
	}
	if err := res.Body.Close(); err != nil {
		return false, fmt.Errorf("close metadata response for %s: %w", url, err)
	}
	if res.StatusCode == http.StatusNotFound {
		g.log.Println("No action metadata at", url)
		return false, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false, fmt.Errorf("check metadata for %s: HTTP %d", url, res.StatusCode)
	}
	return true, nil
}

func (g *gen) discoverVersions(ctx context.Context) ([]*registry, []string, error) {
	actions, err := g.registry()
	if err != nil {
		return nil, nil, err
	}
	refsByRepo, err := g.fetchRefs(ctx, actions)
	if err != nil {
		return nil, nil, err
	}
	added := []string{}
	for _, action := range actions {
		if action.VersionPattern == "" {
			continue
		}
		re, err := versionPattern(action.VersionPattern)
		if err != nil {
			return nil, nil, err
		}
		latest := []uint64{}
		for _, tag := range action.Tags {
			version, err := refVersion(re, tag)
			if err != nil {
				return nil, nil, err
			}
			if slices.Compare(version, latest) > 0 {
				latest = version
			}
		}
		if len(latest) == 0 {
			return nil, nil, fmt.Errorf("no tracked version matches the pattern for %s", action.Slug)
		}
		type candidate struct {
			ref     string
			version []uint64
		}
		candidates := []candidate{}
		for _, ref := range refsByRepo[action.Slug] {
			version, err := refVersion(re, ref)
			if err != nil {
				return nil, nil, err
			}
			// Do not resurrect old versions deliberately omitted from the registry.
			if slices.Compare(version, latest) > 0 {
				candidates = append(candidates, candidate{ref: ref, version: version})
			}
		}
		slices.SortFunc(candidates, func(a, b candidate) int { return slices.Compare(a.version, b.version) })
		for _, candidate := range candidates {
			exists, err := g.metadataExists(ctx, action.rawURL(candidate.ref))
			if err != nil {
				return nil, nil, err
			}
			if !exists {
				continue
			}
			action.Tags = append(action.Tags, candidate.ref)
			added = append(added, action.spec(candidate.ref))
		}
	}
	return actions, added, nil
}

// Write only after discovery succeeds for every repository. A failed fetch must
// not leave a partially updated registry or discard its previous contents.
func writeRegistry(path string, actions []*registry) error {
	data, err := json.MarshalIndent(actions, "", "    ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".popular-actions-*.json")
	if err != nil {
		return fmt.Errorf("create temporary registry: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("write registry: %w", err)
	}
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return fmt.Errorf("set registry permissions: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close registry: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace registry: %w", err)
	}
	return nil
}
