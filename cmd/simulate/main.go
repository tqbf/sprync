package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/tqbf/sprync/pkg/harness"
	"github.com/tqbf/sprync/pkg/pack"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	spryncdBin, err := buildSpryncd()
	if err != nil {
		return err
	}
	defer os.Remove(spryncdBin)

	localDir, err := os.MkdirTemp("", "sprync-local-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(localDir)

	remoteDir, err := os.MkdirTemp("", "sprync-remote-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(remoteDir)

	fmt.Println("=== Building directory trees ===")
	fmt.Printf("Local:  %s\n", localDir)
	fmt.Printf("Remote: %s\n\n", remoteDir)

	buildLocalTree(localDir)
	buildRemoteTree(remoteDir)

	localCount := countFiles(localDir)
	remoteCount := countFiles(remoteDir)
	fmt.Printf("Local files:  %d\n", localCount)
	fmt.Printf("Remote files: %d\n\n", remoteCount)

	fmt.Println("=== Starting spryncd session ===")
	sess, err := harness.Start(spryncdBin)
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer sess.Quit()
	fmt.Printf(
		"spryncd ready (version=%s, pid=%d)\n\n",
		sess.Version, sess.PID,
	)

	excludes := []string{
		"node_modules",
		".git",
		"*.pyc",
		"__pycache__",
		".DS_Store",
		"*.swp",
		"build/",
		".env",
		".env.*",
	}

	fmt.Println("=== Remote manifest ===")
	remoteEntries, exists, elapsed, err := sess.Manifest(
		remoteDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("remote manifest: %w", err)
	}
	fmt.Printf(
		"exists=%t entries=%d elapsed=%s\n",
		exists, len(remoteEntries), elapsed,
	)

	remoteManifest := toManifest(remoteEntries)

	fmt.Println("\n=== Local manifest ===")
	localManifest, err := pack.WalkLocal(localDir, excludes)
	if err != nil {
		return fmt.Errorf("local manifest: %w", err)
	}
	fmt.Printf("entries=%d\n", len(localManifest))

	fmt.Println("\n=== Computing diff (push mode, --delete) ===")
	diff := pack.ComputeDiff(localManifest, remoteManifest, true)

	newFiles := []string{}
	changedFiles := []string{}
	for _, p := range diff.Uploads {
		if _, inRemote := remoteManifest[p]; inRemote {
			changedFiles = append(changedFiles, p)
		} else {
			newFiles = append(newFiles, p)
		}
	}

	var totalUploadBytes int64
	for _, p := range diff.Uploads {
		if e, ok := localManifest[p]; ok {
			totalUploadBytes += e.Size
		}
	}

	fmt.Printf("\n--- New files (%d) ---\n", len(newFiles))
	printPaths(newFiles, localManifest, "+")

	fmt.Printf("\n--- Changed files (%d) ---\n", len(changedFiles))
	printPaths(changedFiles, localManifest, "~")

	fmt.Printf("\n--- Deleted files (%d) ---\n", len(diff.Deletes))
	for _, p := range diff.Deletes {
		fmt.Printf("  - %s\n", p)
	}

	unchanged := 0
	for p := range localManifest {
		if re, ok := remoteManifest[p]; ok {
			if localManifest[p].Hash == re.Hash {
				unchanged++
			}
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("  New:       %d files\n", len(newFiles))
	fmt.Printf("  Changed:   %d files\n", len(changedFiles))
	fmt.Printf("  Deleted:   %d files\n", len(diff.Deletes))
	fmt.Printf("  Unchanged: %d files\n", unchanged)
	fmt.Printf(
		"  Upload:    %d files (%s)\n",
		len(diff.Uploads), humanBytes(totalUploadBytes),
	)

	if len(diff.Uploads) == 0 && len(diff.Deletes) == 0 {
		fmt.Println("\n  Already in sync!")
		return nil
	}

	fmt.Println("\n=== Simulating push ===")

	if len(diff.Uploads) > 0 {
		dest := "/tmp/sprync-" + randHex(8) + ".tar.gz"
		fmt.Printf(
			"  [stub] pack %d files → %s\n",
			len(diff.Uploads), dest,
		)
		result, err := sess.Pack(
			remoteDir, diff.Uploads, dest, true,
		)
		if err != nil {
			return fmt.Errorf("pack: %w", err)
		}
		fmt.Printf(
			"  pack_done: count=%d dest=%s\n",
			result.Count, result.Dest,
		)

		fmt.Printf("  [stub] extract %s → %s\n", dest, remoteDir)
		extractResult, err := sess.Extract(
			remoteDir, dest, true,
		)
		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		fmt.Printf(
			"  extract_done: count=%d\n",
			extractResult.Count,
		)
	}

	if len(diff.Deletes) > 0 {
		fmt.Printf(
			"  [stub] delete %d files from %s\n",
			len(diff.Deletes), remoteDir,
		)
		delResult, err := sess.Delete(
			remoteDir, diff.Deletes,
		)
		if err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		fmt.Printf(
			"  delete_done: count=%d\n",
			delResult.Count,
		)
	}

	fmt.Println("\n=== Simulating second sync (should be no-op) ===")
	fmt.Println("  (re-reading both manifests from same dirs)")

	remoteEntries2, _, _, err := sess.Manifest(
		remoteDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("remote manifest 2: %w", err)
	}
	remoteManifest2 := toManifest(remoteEntries2)

	localManifest2, err := pack.WalkLocal(localDir, excludes)
	if err != nil {
		return fmt.Errorf("local manifest 2: %w", err)
	}

	diff2 := pack.ComputeDiff(
		localManifest2, remoteManifest2, true,
	)
	if len(diff2.Uploads) == 0 && len(diff2.Deletes) == 0 {
		fmt.Println("  Still in sync (trees unchanged).")
		fmt.Println("  Note: stubs don't modify remote,")
		fmt.Println("  so diff reflects the same delta.")
	} else {
		fmt.Printf(
			"  Uploads: %d, Deletes: %d\n",
			len(diff2.Uploads), len(diff2.Deletes),
		)
		fmt.Println(
			"  (Expected: stubs don't actually sync files.)",
		)
	}

	fmt.Println("\n=== Simulating blind push (new remote dir) ===")
	blindDir := filepath.Join(remoteDir, "brand-new-project")
	entries, blindExists, _, err := sess.Manifest(
		blindDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("blind manifest: %w", err)
	}
	fmt.Printf(
		"  manifest: exists=%t entries=%d\n",
		blindExists, len(entries),
	)
	if !blindExists {
		allLocal := []string{}
		for p := range localManifest {
			allLocal = append(allLocal, p)
		}
		sort.Strings(allLocal)
		fmt.Printf(
			"  Would upload ALL %d local files\n",
			len(allLocal),
		)
		dest := "/tmp/sprync-" + randHex(8) + ".tar.gz"
		extractResult, err := sess.Extract(
			blindDir, dest, true,
		)
		if err != nil {
			return fmt.Errorf("blind extract: %w", err)
		}
		fmt.Printf(
			"  [stub] extract_done: count=%d\n",
			extractResult.Count,
		)
	}

	fmt.Println("\n=== Simulating pull ===")
	pullRemoteEntries, pullExists, _, err := sess.Manifest(
		remoteDir, excludes,
	)
	if err != nil {
		return fmt.Errorf("pull manifest: %w", err)
	}
	if !pullExists {
		return fmt.Errorf(
			"remote dir does not exist for pull",
		)
	}
	pullRemote := toManifest(pullRemoteEntries)

	emptyLocalDir, err := os.MkdirTemp(
		"", "sprync-pull-local-*",
	)
	if err != nil {
		return err
	}
	defer os.RemoveAll(emptyLocalDir)

	emptyLocal, err := pack.WalkLocal(emptyLocalDir, excludes)
	if err != nil {
		return fmt.Errorf("empty local manifest: %w", err)
	}

	var pullDownloads []string
	for path, re := range pullRemote {
		le, ok := emptyLocal[path]
		if !ok || le.Hash != re.Hash {
			pullDownloads = append(pullDownloads, path)
		}
	}
	sort.Strings(pullDownloads)

	fmt.Printf(
		"  Pull into empty dir: %d files to download\n",
		len(pullDownloads),
	)
	if len(pullDownloads) > 0 {
		dest := "/tmp/sprync-" + randHex(8) + ".tar.gz"
		packResult, err := sess.Pack(
			remoteDir, pullDownloads, dest, true,
		)
		if err != nil {
			return fmt.Errorf("pull pack: %w", err)
		}
		fmt.Printf(
			"  [stub] pack_done: count=%d\n",
			packResult.Count,
		)
	}

	fmt.Println("\n=== Session cleanup ===")
	err = sess.Quit()
	if err != nil {
		return fmt.Errorf("quit: %w", err)
	}
	fmt.Println("  spryncd exited cleanly")
	fmt.Println("\nDone.")
	return nil
}

func buildLocalTree(dir string) {
	files := map[string]fileSpec{
		"main.go": {
			0644,
			goFile("main", `func main() { fmt.Println("hello") }`),
		},
		"go.mod": {0644, "module example.com/myproject\n\ngo 1.22\n"},
		"go.sum": {0644, ""},
		"Makefile": {
			0644,
			"all:\n\tgo build ./...\ntest:\n\tgo test ./...\n",
		},
		"README.md":  {0644, "# My Project\n\nA Go project.\n"},
		"LICENSE":     {0644, "MIT License\n\n..."},
		".gitignore":  {0644, "build/\n*.pyc\n.env\n"},

		"cmd/server/main.go": {
			0644,
			goFile("main", `func main() { http.ListenAndServe(":8080", nil) }`),
		},
		"cmd/worker/main.go": {
			0644,
			goFile("main", `func main() { runWorker() }`),
		},
		"cmd/migrate/main.go": {
			0644,
			goFile("main", `func main() { migrate() }`),
		},

		"pkg/api/handler.go": {
			0644,
			goFile("api", "func HandleRequest(w http.ResponseWriter, r *http.Request) {}"),
		},
		"pkg/api/middleware.go": {
			0644,
			goFile("api", "func AuthMiddleware(next http.Handler) http.Handler { return next }"),
		},
		"pkg/api/router.go": {
			0644,
			goFile("api", "func NewRouter() *http.ServeMux { return http.NewServeMux() }"),
		},
		"pkg/api/handler_test.go": {
			0644,
			goFile("api", "func TestHandleRequest(t *testing.T) {}"),
		},

		"pkg/db/postgres.go": {
			0644,
			goFile("db", "type DB struct { connStr string }"),
		},
		"pkg/db/migrations.go": {
			0644,
			goFile("db", "func RunMigrations(db *DB) error { return nil }"),
		},
		"pkg/db/models.go": {
			0644,
			goFile("db", "type User struct { ID int; Name string; Email string }"),
		},

		"pkg/worker/queue.go": {
			0644,
			goFile("worker", "type Queue struct { jobs chan Job }"),
		},
		"pkg/worker/processor.go": {
			0644,
			goFile("worker", "func Process(j Job) error { return nil }"),
		},

		"pkg/auth/jwt.go": {
			0644,
			goFile("auth", "func GenerateToken(userID int) (string, error) { return \"\", nil }"),
		},
		"pkg/auth/oauth.go": {
			0644,
			goFile("auth", "func OAuthCallback(provider string) error { return nil }"),
		},
		"pkg/auth/password.go": {
			0644,
			goFile("auth", "func HashPassword(pw string) (string, error) { return \"\", nil }"),
		},

		"pkg/config/config.go": {
			0644,
			goFile("config", "type Config struct { Port int; DBUrl string; Debug bool }"),
		},
		"pkg/config/env.go": {
			0644,
			goFile("config", "func LoadFromEnv() (*Config, error) { return nil, nil }"),
		},

		"internal/cache/redis.go": {
			0644,
			goFile("cache", "type RedisCache struct { addr string }"),
		},
		"internal/cache/memory.go": {
			0644,
			goFile("cache", "type MemCache struct { data map[string][]byte }"),
		},

		"internal/email/sender.go": {
			0644,
			goFile("email", "func Send(to, subject, body string) error { return nil }"),
		},
		"internal/email/template.go": {
			0644,
			goFile("email", "func RenderTemplate(name string, data any) (string, error) { return \"\", nil }"),
		},

		"web/static/css/app.css": {
			0644,
			"body { margin: 0; font-family: sans-serif; }\n.container { max-width: 1200px; margin: 0 auto; }\n",
		},
		"web/static/css/reset.css": {
			0644,
			"* { box-sizing: border-box; }\n",
		},
		"web/static/js/app.js": {
			0644,
			"document.addEventListener('DOMContentLoaded', () => {\n  console.log('loaded');\n});\n",
		},
		"web/static/js/api.js": {
			0644,
			"async function fetchData(url) {\n  const resp = await fetch(url);\n  return resp.json();\n}\n",
		},
		"web/templates/layout.html": {
			0644,
			"<!DOCTYPE html><html><head><title>{{.Title}}</title></head><body>{{.Content}}</body></html>",
		},
		"web/templates/index.html": {
			0644,
			"{{define \"content\"}}<h1>Welcome</h1>{{end}}",
		},
		"web/templates/login.html": {
			0644,
			"{{define \"content\"}}<form method=POST><input name=email><input name=password type=password><button>Login</button></form>{{end}}",
		},

		"scripts/deploy.sh":  {0755, "#!/bin/bash\nset -e\necho 'deploying...'\n"},
		"scripts/seed.sh":    {0755, "#!/bin/bash\nset -e\necho 'seeding db...'\n"},
		"scripts/backup.sh":  {0755, "#!/bin/bash\nset -e\necho 'backing up...'\n"},

		"docs/api.md":         {0644, "# API Reference\n\n## Endpoints\n\n### GET /api/v1/users\n"},
		"docs/architecture.md": {0644, "# Architecture\n\n## Components\n\n### API Server\n### Worker\n### Database\n"},
		"docs/deploy.md":      {0644, "# Deployment\n\n## Prerequisites\n\n## Steps\n"},

		"migrations/001_users.sql":    {0644, "CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT, email TEXT UNIQUE);\n"},
		"migrations/002_sessions.sql": {0644, "CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id INT REFERENCES users(id), expires_at TIMESTAMPTZ);\n"},
		"migrations/003_jobs.sql":     {0644, "CREATE TABLE jobs (id SERIAL PRIMARY KEY, type TEXT, payload JSONB, status TEXT DEFAULT 'pending');\n"},

		"testdata/fixtures/users.json": {
			0644,
			`[{"id":1,"name":"Alice","email":"alice@example.com"},{"id":2,"name":"Bob","email":"bob@example.com"}]`,
		},
		"testdata/fixtures/jobs.json": {
			0644,
			`[{"type":"email","payload":{"to":"alice@example.com"}},{"type":"notify","payload":{"msg":"hello"}}]`,
		},

		"node_modules/leftpad/index.js":         {0644, "module.exports = function leftpad(s,n) { return s.padStart(n); }"},
		"node_modules/leftpad/package.json":      {0644, `{"name":"leftpad","version":"1.0.0"}`},
		".git/config":                            {0644, "[core]\n\trepositoryformatversion = 0\n"},
		".git/HEAD":                              {0644, "ref: refs/heads/main\n"},
		"build/server":                           {0755, "ELF binary stub"},
		"__pycache__/util.cpython-312.pyc":       {0644, "python bytecode stub"},
		".env":                                   {0600, "DATABASE_URL=postgres://localhost/mydb\nSECRET_KEY=hunter2\n"},
		".env.local":                             {0600, "DEBUG=true\n"},
		".DS_Store":                              {0644, "macOS junk"},

		"深い/ディレクトリ/テスト.txt": {0644, "unicode path test"},
		"café/résumé.md":          {0644, "accent test"},
		"file with spaces.txt":    {0644, "spaces in name"},
	}

	for path, spec := range files {
		writeFile(dir, path, spec)
	}
}

func buildRemoteTree(dir string) {
	files := map[string]fileSpec{
		"main.go": {
			0644,
			goFile("main", `func main() { fmt.Println("hello") }`),
		},
		"go.mod":     {0644, "module example.com/myproject\n\ngo 1.22\n"},
		"go.sum":     {0644, ""},
		"Makefile": {
			0644,
			"all:\n\tgo build ./...\ntest:\n\tgo test ./...\n",
		},
		"README.md": {0644, "# My Project\n\nOld description.\n"},
		"LICENSE":    {0644, "MIT License\n\n..."},

		"cmd/server/main.go": {
			0644,
			goFile("main", `func main() { http.ListenAndServe(":3000", nil) }`),
		},
		"cmd/worker/main.go": {
			0644,
			goFile("main", `func main() { runWorker() }`),
		},

		"pkg/api/handler.go": {
			0644,
			goFile("api", "func HandleRequest(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }"),
		},
		"pkg/api/middleware.go": {
			0644,
			goFile("api", "func AuthMiddleware(next http.Handler) http.Handler { return next }"),
		},
		"pkg/api/router.go": {
			0644,
			goFile("api", "func NewRouter() *http.ServeMux { return http.NewServeMux() }"),
		},
		"pkg/api/deprecated.go": {
			0644,
			goFile("api", "func OldHandler() {} // deprecated"),
		},

		"pkg/db/postgres.go": {
			0644,
			goFile("db", "type DB struct { connStr string }"),
		},
		"pkg/db/migrations.go": {
			0644,
			goFile("db", "func RunMigrations(db *DB) error { return nil }"),
		},
		"pkg/db/models.go": {
			0644,
			goFile("db", "type User struct { ID int; Name string }"),
		},

		"pkg/worker/queue.go": {
			0644,
			goFile("worker", "type Queue struct { jobs chan Job }"),
		},
		"pkg/worker/processor.go": {
			0644,
			goFile("worker", "func Process(j Job) error { return nil }"),
		},

		"pkg/auth/jwt.go": {
			0644,
			goFile("auth", "func GenerateToken(userID int) (string, error) { return \"\", nil }"),
		},

		"pkg/config/config.go": {
			0644,
			goFile("config", "type Config struct { Port int; DBUrl string }"),
		},

		"internal/cache/redis.go": {
			0644,
			goFile("cache", "type RedisCache struct { addr string }"),
		},

		"web/static/css/app.css": {
			0644,
			"body { margin: 0; }\n",
		},
		"web/static/js/app.js": {
			0644,
			"console.log('old version');\n",
		},
		"web/templates/layout.html": {
			0644,
			"<!DOCTYPE html><html><head><title>{{.Title}}</title></head><body>{{.Content}}</body></html>",
		},
		"web/templates/index.html": {
			0644,
			"{{define \"content\"}}<h1>Home</h1>{{end}}",
		},

		"scripts/deploy.sh": {0755, "#!/bin/bash\necho 'old deploy'\n"},
		"scripts/seed.sh":   {0755, "#!/bin/bash\nset -e\necho 'seeding db...'\n"},

		"docs/api.md": {0644, "# API Reference\n\n## Endpoints\n"},

		"migrations/001_users.sql": {
			0644,
			"CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT, email TEXT UNIQUE);\n",
		},
		"migrations/002_sessions.sql": {
			0644,
			"CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id INT REFERENCES users(id), expires_at TIMESTAMPTZ);\n",
		},

		"old_code/legacy.go": {
			0644,
			goFile("legacy", "func OldStuff() {}"),
		},
		"old_code/compat.go": {
			0644,
			goFile("legacy", "func Compat() {}"),
		},
		"tmp_notes.txt": {0644, "TODO: clean this up\n"},

		"node_modules/leftpad/index.js": {
			0644,
			"module.exports = function leftpad(s,n) { return s.padStart(n); }",
		},
		".git/config": {0644, "[core]\n\trepositoryformatversion = 0\n"},
		".git/HEAD":   {0644, "ref: refs/heads/main\n"},
	}

	for path, spec := range files {
		writeFile(dir, path, spec)
	}
}

type fileSpec struct {
	mode    os.FileMode
	content string
}

func writeFile(base, rel string, spec fileSpec) {
	full := filepath.Join(base, rel)
	os.MkdirAll(filepath.Dir(full), 0755)
	os.WriteFile(full, []byte(spec.content), 0644)
	os.Chmod(full, spec.mode)
}

func goFile(pkg, body string) string {
	return fmt.Sprintf("package %s\n\n%s\n", pkg, body)
}

func countFiles(dir string) int {
	n := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n
}

func printPaths(
	paths []string,
	manifest pack.Manifest,
	prefix string,
) {
	for _, p := range paths {
		if e, ok := manifest[p]; ok {
			fmt.Printf(
				"  %s %s (%s)\n", prefix, p, humanBytes(e.Size),
			)
		} else {
			fmt.Printf("  %s %s\n", prefix, p)
		}
	}
}

func toManifest(entries []pack.ManifestEntry) pack.Manifest {
	m := make(pack.Manifest, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func buildSpryncd() (string, error) {
	tmp, err := os.MkdirTemp("", "spryncd-build-*")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(tmp, "spryncd")

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	modRoot := findModRoot(wd)

	cmd := exec.Command(
		"go", "build", "-o", bin, "./cmd/spryncd",
	)
	cmd.Dir = modRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf(
			"build spryncd: %s\n%s", err, out,
		)
	}
	return bin, nil
}

func findModRoot(dir string) string {
	for {
		if _, err := os.Stat(
			filepath.Join(dir, "go.mod"),
		); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

