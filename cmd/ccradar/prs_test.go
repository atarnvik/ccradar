package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func prLine(n int, repo string) string {
	return `{"type":"pr-link","prNumber":` + strconv.Itoa(n) + `,"prUrl":"https://github.com/` + repo + `/pull/` + strconv.Itoa(n) + `","prRepository":"` + repo + `","timestamp":"2026-09-01T10:00:00.000Z"}`
}

func TestScanPRLinksDedupesLatestLast(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeTranscript(t, p, time.Now(),
		`{"type":"user","cwd":"/w","timestamp":"2026-09-01T09:00:00.000Z","message":{"role":"user","content":"hi"}}`,
		prLine(10, "o/r"), prLine(11, "o/r"), prLine(10, "o/r"))
	prs := scanTranscript(p).details.PRs
	if len(prs) != 2 || prs[0].Number != 11 || prs[1].Number != 10 {
		t.Fatalf("PRs = %+v, want #11 then #10 (re-linked PR moves to the end)", prs)
	}
	if l, _ := (sessionDetails{PRs: prs}).latestPR(); l.Number != 10 || l.Repo != "o/r" {
		t.Errorf("latest = %+v", l)
	}
}

func TestDuePRs(t *testing.T) {
	now := time.Now()
	links := []prLink{{URL: "new"}, {URL: "fresh"}, {URL: "stale"}, {URL: "merged"}, {URL: "new"}}
	have := map[string]prInfo{
		"fresh":  {State: "OPEN", fetched: now.Add(-time.Minute)},
		"stale":  {State: "OPEN", fetched: now.Add(-prRefresh - time.Second)},
		"merged": {State: "MERGED", fetched: now.Add(-24 * time.Hour)},
	}
	var got []string
	for _, l := range duePRs(links, have, now) {
		got = append(got, l.URL)
	}
	if strings.Join(got, ",") != "new,stale" {
		t.Fatalf("due = %v, want new,stale (fresh and final PRs skipped, deduped)", got)
	}
}

func TestPRQuerySkipsMalformedRepos(t *testing.T) {
	q, aliases := prQuery([]prLink{
		{URL: "a", Repo: "acme/app", Number: 5691},
		{URL: "b", Repo: `evil") { x } y: repository(owner: "z`, Number: 1},
		{URL: "c", Repo: "noslash", Number: 2},
	})
	if len(aliases) != 1 || aliases["p0"] != "a" {
		t.Fatalf("aliases = %v", aliases)
	}
	if !strings.Contains(q, `p0: repository(owner: "acme", name: "app") { pullRequest(number: 5691)`) || strings.Contains(q, "evil") {
		t.Fatalf("query = %s", q)
	}
	if q, _ := prQuery([]prLink{{URL: "c", Repo: "noslash"}}); q != "" {
		t.Errorf("query for no valid links = %q, want empty", q)
	}
}

const ghResponse = `{"data":{
 "p0":{"pullRequest":{"state":"OPEN","isDraft":false,"title":"Search tweaks page","reviewDecision":"APPROVED","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}}},
 "p1":{"pullRequest":{"state":"MERGED","isDraft":false,"title":"Tables","reviewDecision":null,"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}},
 "p2":null},
 "errors":[{"type":"NOT_FOUND","path":["p2"]}]}`

func TestParsePRResponse(t *testing.T) {
	got := parsePRResponse([]byte(ghResponse), map[string]string{"p0": "u0", "p1": "u1", "p2": "u2"})
	if got["u0"] != (prInfo{State: "OPEN", Title: "Search tweaks page", Checks: "FAILURE", Review: "APPROVED"}) {
		t.Errorf("u0 = %+v", got["u0"])
	}
	if got["u1"].State != "MERGED" || got["u1"].Checks != "" {
		t.Errorf("u1 = %+v", got["u1"])
	}
	if _, ok := got["u2"]; ok {
		t.Error("inaccessible PR should be absent")
	}
	if len(parsePRResponse([]byte("not json"), nil)) != 0 {
		t.Error("garbage should parse to nothing")
	}
}

func TestFetchPRsWithFakeGh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "gh")
	// Like real gh with an inaccessible PR in the batch: prints data, exits 1.
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args\"\ncat <<'JSON'\n" + ghResponse + "\nJSON\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCRADAR_GH", fake)

	links := []prLink{{URL: "u0", Repo: "o/r", Number: 1}, {URL: "u1", Repo: "o/r", Number: 2}, {URL: "u2", Repo: "o/r", Number: 3}}
	got := fetchPRs(links)
	if got["u0"].State != "OPEN" || got["u1"].State != "MERGED" {
		t.Fatalf("got = %+v", got)
	}
	if info, ok := got["u2"]; !ok || info.State != "" || info.fetched.IsZero() {
		t.Errorf("failed lookup should still be recorded (to back off): %+v", info)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if !strings.HasPrefix(string(args), "api\ngraphql\n-f\nquery=query { p0: repository(") {
		t.Errorf("args = %q", args)
	}
}

func TestPRBadgeAndPreviewLine(t *testing.T) {
	d := sessionDetails{PRs: []prLink{{URL: "u", Number: 5691}}}
	cases := map[string]struct {
		info    prInfo
		plain   string
		preview string
	}{
		"unknown": {prInfo{}, "#5691     ", "#5691   "},
		"passing": {prInfo{State: "OPEN", Checks: "SUCCESS", Title: "Search tweaks"}, "#5691 ✓   ", "#5691 ✓ Search tweaks"},
		"failing": {prInfo{State: "OPEN", Checks: "FAILURE", Title: "Search tweaks"}, "#5691 ✗   ", "#5691 ✗ Search tweaks"},
		"draft":   {prInfo{State: "OPEN", Draft: true, Checks: "PENDING", Title: "Search tweaks"}, "#5691 •   ", "#5691 • Search tweaks"},
		"merged":  {prInfo{State: "MERGED", Checks: "SUCCESS", Title: "Search tweaks"}, "#5691 ⑂   ", "#5691 ⑂ Search tweaks"},
		"closed":  {prInfo{State: "CLOSED", Checks: "FAILURE"}, "#5691 ×   ", "#5691 × closed"},
	}
	for name, c := range cases {
		plain, colored := prBadge(d, map[string]prInfo{"u": c.info})
		if plain != c.plain || lipgloss.Width(colored) != prColumnWidth || len([]rune(plain)) != prColumnWidth {
			t.Errorf("%s: badge = %q (colored width %d)", name, plain, lipgloss.Width(colored))
		}
		if got := prPreviewLine(d.PRs[0], c.info, 5, 40).plain; got != c.preview {
			t.Errorf("%s: preview line = %q, want %q", name, got, c.preview)
		}
	}
	if plain, _ := prBadge(sessionDetails{}, nil); plain != strings.Repeat(" ", prColumnWidth) {
		t.Errorf("no-PR badge = %q, want blank", plain)
	}
}

func TestPRStatusFlow(t *testing.T) {
	t.Setenv("CCRADAR_NO_PR_STATUS", "")
	t.Setenv("CCRADAR_DEMO", "")
	link := prLink{URL: "u", Repo: "o/r", Number: 7}
	m := model{width: 120, height: 20}
	tm, cmd := m.Update(loadedMsg{{SessionID: "s", CWD: "/w", Status: "idle", SurfaceID: "t",
		sessionDetails: sessionDetails{PRs: []prLink{link}}}})
	m = tm.(model)
	if !m.prBusy || cmd == nil {
		t.Fatal("a session with an unknown PR should start a lookup")
	}
	if !m.anyPR {
		t.Error("anyPR not set for a list with a PR")
	}
	tm, _ = m.Update(prStatusMsg{"u": {State: "OPEN", fetched: time.Now()}})
	m = tm.(model)
	if m.prBusy || m.prStatus["u"].State != "OPEN" {
		t.Fatalf("status not applied: busy=%v status=%+v", m.prBusy, m.prStatus)
	}
	if m.maybeFetchPRs() != nil {
		t.Error("fresh status re-fetched")
	}

	t.Setenv("CCRADAR_NO_PR_STATUS", "1")
	m.prStatus = nil
	if m.maybeFetchPRs() != nil {
		t.Error("CCRADAR_NO_PR_STATUS=1 should disable lookups")
	}
}

// A fake gh with two accounts: "me" (active) sees only owner "me", "work" sees
// only owner "work". Every graphql call is logged as "<token> <owners…>".
func fakeMultiAccountGh(t *testing.T) (logPath string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls")
	script := `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  echo '{"hosts":{"github.com":[{"login":"work","state":"success","active":false},{"login":"me","state":"success","active":true},{"login":"broken","state":"error","active":false}]}}'
  exit 1
fi
if [ "$1 $2" = "auth token" ]; then echo "tok-$4"; exit 0; fi
query="$4"
owners=$(printf '%s' "$query" | grep -o 'owner: "[^"]*"' | sed 's/owner: "\(.*\)"/\1/' | tr '\n' ' ')
echo "$GH_TOKEN $owners" >> "` + logPath + `"
printf '{"data":{'
i=0; sep=""
for o in $owners; do
  if [ "tok-$o" = "$GH_TOKEN" ]; then
    printf '%s"p%d":{"pullRequest":{"state":"OPEN","title":"%s pr","isDraft":false,"commits":{"nodes":[]}}}' "$sep" $i "$o"
  else
    printf '%s"p%d":null' "$sep" $i
  fi
  i=$((i+1)); sep=","
done
printf '}}\n'
exit 1
`
	fake := filepath.Join(dir, "gh")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCRADAR_GH", fake)
	t.Setenv("GH_TOKEN", "inherited") // must not leak into per-account calls
	ownerAccounts.Lock()
	ownerAccounts.m = map[string]string{}
	ownerAccounts.Unlock()
	return logPath
}

func TestFetchPRsAcrossAccounts(t *testing.T) {
	logPath := fakeMultiAccountGh(t)
	links := []prLink{
		{URL: "u-me", Repo: "me/app", Number: 1},
		{URL: "u-work", Repo: "work/app", Number: 2},
		{URL: "u-gone", Repo: "nobody/app", Number: 3},
	}

	got := fetchPRs(links)
	if got["u-me"].Title != "me pr" || got["u-work"].Title != "work pr" {
		t.Fatalf("got = %+v, want each PR from the account that can see it", got)
	}
	if info := got["u-gone"]; info.State != "" || info.fetched.IsZero() {
		t.Errorf("PR no account can see should be recorded empty (to back off): %+v", info)
	}
	calls, _ := os.ReadFile(logPath)
	want := "tok-me me work nobody \ntok-work work nobody \n"
	if string(calls) != want {
		t.Fatalf("first refresh calls:\n%s\nwant (active account first, then the rest retried):\n%s", calls, want)
	}

	// Next refresh: owners are remembered, so each account gets only its own.
	_ = os.Remove(logPath)
	fetchPRs(links[:2])
	calls, _ = os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	sort.Strings(lines) // the accounts are asked in parallel
	if got, want := strings.Join(lines, "|"), "tok-me me|tok-work work"; got != want {
		t.Fatalf("second refresh calls: %s\nwant one call per account, no misses: %s", got, want)
	}
}
