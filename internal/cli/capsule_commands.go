package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/capsule"
)

type capsuleListItem struct {
	ID             string    `json:"id"`
	Path           string    `json:"path"`
	AgentName      string    `json:"agentName"`
	SourceClawfile string    `json:"sourceClawfile"`
	CreatedAt      time.Time `json:"createdAt"`
}

type capsuleMaterial struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	AgentName string `json:"agentName"`

	IR     any `json:"ir"`
	Policy any `json:"policy"`
	Deps   any `json:"depsLock"`
	Image  any `json:"imageLock"`
	Source any `json:"sourceLock"`
}

type capsuleDiffResult struct {
	Left     capsuleDiffRef `json:"left"`
	Right    capsuleDiffRef `json:"right"`
	Sections []sectionDiff  `json:"sections"`
	Equal    bool           `json:"equal"`
}

type capsuleDiffRef struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	AgentName string `json:"agentName"`
}

type sectionDiff struct {
	Section string       `json:"section"`
	Added   []jsonChange `json:"added,omitempty"`
	Removed []jsonChange `json:"removed,omitempty"`
	Changed []jsonChange `json:"changed,omitempty"`
	Equal   bool         `json:"equal"`
}

type jsonChange struct {
	Path string `json:"path"`
	Old  any    `json:"old,omitempty"`
	New  any    `json:"new,omitempty"`
}

func runCapsule(args []string) int {
	if len(args) == 0 {
		printCapsuleUsage()
		return 1
	}
	switch args[0] {
	case "list":
		return runCapsuleList(args[1:])
	case "diff":
		return runCapsuleDiff(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown capsule subcommand: %s\n", args[0])
		printCapsuleUsage()
		return 1
	}
}

func runCapsuleList(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--state-dir": true,
		"--agent":     true,
		"--since":     true,
		"--until":     true,
		"--limit":     true,
	})

	fs := flag.NewFlagSet("capsule list", flag.ContinueOnError)
	var stateDir string
	var agentFilter string
	var sinceRaw string
	var untilRaw string
	var limit int
	var asJSON bool
	fs.StringVar(&stateDir, "state-dir", ".metaclaw", "state directory")
	fs.StringVar(&agentFilter, "agent", "", "filter by agent name (contains, case-insensitive)")
	fs.StringVar(&sinceRaw, "since", "", "created at lower bound (RFC3339 or YYYY-MM-DD)")
	fs.StringVar(&untilRaw, "until", "", "created at upper bound (RFC3339 or YYYY-MM-DD)")
	fs.IntVar(&limit, "limit", 100, "max rows")
	fs.BoolVar(&asJSON, "json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw capsule list [--state-dir=.metaclaw] [--agent=...] [--since=...] [--until=...]")
		return 1
	}

	since, hasSince, err := parseTimeFilter(sinceRaw, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --since value: %v\n", err)
		return 1
	}
	until, hasUntil, err := parseTimeFilter(untilRaw, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --until value: %v\n", err)
		return 1
	}

	capsuleRoot := filepath.Join(stateDir, "capsules")
	items, err := discoverCapsules(capsuleRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capsule list failed: %v\n", err)
		return 1
	}
	items = filterCapsules(items, strings.TrimSpace(agentFilter), hasSince, since, hasUntil, until)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	if asJSON {
		b, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	for _, it := range items {
		fmt.Printf("%s\t%s\t%s\t%s\n", it.ID, it.CreatedAt.Format(time.RFC3339), it.AgentName, it.Path)
	}
	return 0
}

func runCapsuleDiff(args []string) int {
	args = reorderFlags(args, map[string]bool{"--state-dir": true})

	fs := flag.NewFlagSet("capsule diff", flag.ContinueOnError)
	var stateDir string
	var asJSON bool
	fs.StringVar(&stateDir, "state-dir", ".metaclaw", "state directory")
	fs.BoolVar(&asJSON, "json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	remaining := fs.Args()
	if len(remaining) != 2 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw capsule diff <id-or-path-1> <id-or-path-2> [--state-dir=.metaclaw] [--json]")
		return 1
	}

	left, err := resolveCapsuleRef(stateDir, remaining[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve %q failed: %v\n", remaining[0], err)
		return 1
	}
	right, err := resolveCapsuleRef(stateDir, remaining[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve %q failed: %v\n", remaining[1], err)
		return 1
	}

	res := diffCapsules(left, right)
	if asJSON {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	fmt.Printf("left:  %s\t%s\t%s\n", res.Left.ID, res.Left.AgentName, res.Left.Path)
	fmt.Printf("right: %s\t%s\t%s\n", res.Right.ID, res.Right.AgentName, res.Right.Path)
	for _, sec := range res.Sections {
		if sec.Equal {
			fmt.Printf("[%s] equal\n", sec.Section)
			continue
		}
		fmt.Printf("[%s] added=%d removed=%d changed=%d\n", sec.Section, len(sec.Added), len(sec.Removed), len(sec.Changed))
		for _, c := range sec.Added {
			fmt.Printf("+ %s = %s\n", c.Path, renderJSONValue(c.New))
		}
		for _, c := range sec.Removed {
			fmt.Printf("- %s = %s\n", c.Path, renderJSONValue(c.Old))
		}
		for _, c := range sec.Changed {
			fmt.Printf("~ %s: %s -> %s\n", c.Path, renderJSONValue(c.Old), renderJSONValue(c.New))
		}
	}
	if res.Equal {
		fmt.Println("capsule diff: no differences across ir/policy/locks")
	}
	return 0
}

func printCapsuleUsage() {
	fmt.Print(`metaclaw capsule commands:
  capsule list [--state-dir=.metaclaw] [--agent=...] [--since=...] [--until=...] [--json]
  capsule diff <id-or-path-1> <id-or-path-2> [--state-dir=.metaclaw] [--json]
`)
}

func discoverCapsules(capsuleRoot string) ([]capsuleListItem, error) {
	entries, err := os.ReadDir(capsuleRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []capsuleListItem{}, nil
		}
		return nil, err
	}

	items := make([]capsuleListItem, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cap_") {
			continue
		}
		capPath := filepath.Join(capsuleRoot, entry.Name())
		manifest, err := capsule.Load(capPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping invalid capsule %s: %v\n", capPath, err)
			continue
		}
		agentName, err := readCapsuleAgentName(capPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to read agent name for %s: %v\n", capPath, err)
		}
		st, err := os.Stat(capPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: stat failed for %s: %v\n", capPath, err)
			continue
		}
		items = append(items, capsuleListItem{
			ID:             manifest.CapsuleID,
			Path:           capPath,
			AgentName:      agentName,
			SourceClawfile: manifest.SourceClawfile,
			CreatedAt:      st.ModTime().UTC(),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func filterCapsules(items []capsuleListItem, agentFilter string, hasSince bool, since time.Time, hasUntil bool, until time.Time) []capsuleListItem {
	out := make([]capsuleListItem, 0, len(items))
	agentFilter = strings.ToLower(agentFilter)
	for _, item := range items {
		if agentFilter != "" && !strings.Contains(strings.ToLower(item.AgentName), agentFilter) {
			continue
		}
		if hasSince && item.CreatedAt.Before(since) {
			continue
		}
		if hasUntil && item.CreatedAt.After(until) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func parseTimeFilter(raw string, endOfDayForDateOnly bool) (time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	layouts := []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"}
	var err error
	for _, layout := range layouts {
		var t time.Time
		t, err = time.Parse(layout, raw)
		if err == nil {
			if layout == "2006-01-02" && endOfDayForDateOnly {
				t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			}
			return t.UTC(), true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("unsupported time format %q", raw)
}

func resolveCapsuleRef(stateDir, ref string) (capsuleMaterial, error) {
	if st, err := os.Stat(ref); err == nil && st.IsDir() {
		return loadCapsuleMaterial(ref)
	}

	capsuleRoot := filepath.Join(stateDir, "capsules")
	candidateNames := []string{"cap_" + ref}
	if strings.HasPrefix(ref, "cap_") {
		candidateNames = append(candidateNames, ref)
	}
	for _, name := range candidateNames {
		candidatePath := filepath.Join(capsuleRoot, name)
		if st, err := os.Stat(candidatePath); err == nil && st.IsDir() {
			return loadCapsuleMaterial(candidatePath)
		}
	}

	entries, err := os.ReadDir(capsuleRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return capsuleMaterial{}, fmt.Errorf("capsule directory not found: %s", capsuleRoot)
		}
		return capsuleMaterial{}, err
	}

	prefixes := []string{"cap_" + ref}
	if strings.HasPrefix(ref, "cap_") {
		prefixes = append(prefixes, ref)
	}
	matches := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry.Name(), prefix) {
				matches = append(matches, filepath.Join(capsuleRoot, entry.Name()))
				break
			}
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return loadCapsuleMaterial(matches[0])
	}
	if len(matches) > 1 {
		return capsuleMaterial{}, fmt.Errorf("ambiguous capsule reference %q; matches: %s", ref, strings.Join(matches, ", "))
	}

	return capsuleMaterial{}, fmt.Errorf("capsule %q not found in %s", ref, capsuleRoot)
}

func loadCapsuleMaterial(capPath string) (capsuleMaterial, error) {
	m, err := capsule.Load(capPath)
	if err != nil {
		return capsuleMaterial{}, fmt.Errorf("load manifest: %w", err)
	}
	agentName, _ := readCapsuleAgentName(capPath)

	ir, err := readJSONFile(filepath.Join(capPath, "ir.json"))
	if err != nil {
		return capsuleMaterial{}, err
	}
	pol, err := readJSONFile(filepath.Join(capPath, "policy.json"))
	if err != nil {
		return capsuleMaterial{}, err
	}
	deps, err := readJSONFile(filepath.Join(capPath, "locks", "deps.lock.json"))
	if err != nil {
		return capsuleMaterial{}, err
	}
	image, err := readJSONFile(filepath.Join(capPath, "locks", "image.lock.json"))
	if err != nil {
		return capsuleMaterial{}, err
	}
	source, err := readJSONFile(filepath.Join(capPath, "locks", "source.lock.json"))
	if err != nil {
		return capsuleMaterial{}, err
	}

	return capsuleMaterial{
		ID:        m.CapsuleID,
		Path:      capPath,
		AgentName: agentName,
		IR:        ir,
		Policy:    pol,
		Deps:      deps,
		Image:     image,
		Source:    source,
	}, nil
}

func readCapsuleAgentName(capPath string) (string, error) {
	b, err := os.ReadFile(filepath.Join(capPath, "ir.json"))
	if err != nil {
		return "", err
	}
	var ir struct {
		Clawfile struct {
			Agent struct {
				Name string `json:"name"`
			} `json:"agent"`
		} `json:"clawfile"`
	}
	if err := json.Unmarshal(b, &ir); err != nil {
		return "", err
	}
	return ir.Clawfile.Agent.Name, nil
}

func readJSONFile(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return out, nil
}

func diffCapsules(left, right capsuleMaterial) capsuleDiffResult {
	sections := []struct {
		name  string
		left  any
		right any
	}{
		{name: "ir", left: left.IR, right: right.IR},
		{name: "policy", left: left.Policy, right: right.Policy},
		{name: "locks.deps", left: left.Deps, right: right.Deps},
		{name: "locks.image", left: left.Image, right: right.Image},
		{name: "locks.source", left: left.Source, right: right.Source},
	}

	res := capsuleDiffResult{
		Left:     capsuleDiffRef{ID: left.ID, Path: left.Path, AgentName: left.AgentName},
		Right:    capsuleDiffRef{ID: right.ID, Path: right.Path, AgentName: right.AgentName},
		Sections: make([]sectionDiff, 0, len(sections)),
		Equal:    true,
	}
	for _, s := range sections {
		d := diffJSONSection(s.name, s.left, s.right)
		if !d.Equal {
			res.Equal = false
		}
		res.Sections = append(res.Sections, d)
	}
	return res
}

func diffJSONSection(name string, left any, right any) sectionDiff {
	leftFlat := make(map[string]any)
	rightFlat := make(map[string]any)
	flattenJSON("", left, leftFlat)
	flattenJSON("", right, rightFlat)

	keySet := make(map[string]struct{}, len(leftFlat)+len(rightFlat))
	for k := range leftFlat {
		keySet[k] = struct{}{}
	}
	for k := range rightFlat {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := sectionDiff{Section: name, Equal: true}
	for _, k := range keys {
		lv, lok := leftFlat[k]
		rv, rok := rightFlat[k]
		switch {
		case lok && !rok:
			out.Removed = append(out.Removed, jsonChange{Path: k, Old: lv})
			out.Equal = false
		case !lok && rok:
			out.Added = append(out.Added, jsonChange{Path: k, New: rv})
			out.Equal = false
		case !jsonEqual(lv, rv):
			out.Changed = append(out.Changed, jsonChange{Path: k, Old: lv, New: rv})
			out.Equal = false
		}
	}
	return out
}

func flattenJSON(path string, value any, out map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			out[normalizePath(path)] = map[string]any{}
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			next := k
			if path != "" {
				next = path + "." + k
			}
			flattenJSON(next, v[k], out)
		}
	case []any:
		if len(v) == 0 {
			out[normalizePath(path)] = []any{}
			return
		}
		for i, item := range v {
			next := fmt.Sprintf("[%d]", i)
			if path != "" {
				next = fmt.Sprintf("%s[%d]", path, i)
			}
			flattenJSON(next, item, out)
		}
	default:
		out[normalizePath(path)] = v
	}
}

func normalizePath(path string) string {
	if path == "" {
		return "$"
	}
	return path
}

func jsonEqual(left any, right any) bool {
	lb, _ := json.Marshal(left)
	rb, _ := json.Marshal(right)
	return string(lb) == string(rb)
}

func renderJSONValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
