package schema

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	legacyMigrationPath = "initialize/migrate/database"
	migrationPath       = "internal/app/migration/schema/database"
)

// migrationLineRepo is a scratch git repository holding a migration line for
// the check-migration-lines.sh guard to compare against its baseline commit.
type migrationLineRepo struct {
	t   *testing.T
	dir string
}

func newMigrationLineRepo(t *testing.T) *migrationLineRepo {
	t.Helper()
	repo := &migrationLineRepo{t: t, dir: t.TempDir()}
	repo.git("init", "--quiet")
	return repo
}

func (r *migrationLineRepo) git(args ...string) {
	r.t.Helper()
	options := []string{"-c", "user.name=Migration Test", "-c", "user.email=migration@example.test", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + filepath.Join(r.dir, "disabled-hooks")}
	cmd := exec.Command("git", append(options, args...)...)
	cmd.Dir = r.dir
	if output, err := cmd.CombinedOutput(); err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func (r *migrationLineRepo) writeMigration(base, dialect, version string) {
	r.t.Helper()
	path := filepath.Join(r.dir, base, dialect, version+"_fixture.up.sql")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("SELECT 1;\n"), 0644); err != nil {
		r.t.Fatal(err)
	}
}

// commitBaseline commits migrations 02000 and 02001 of both dialects under
// base, the line the guard compares against.
func (r *migrationLineRepo) commitBaseline(base string) {
	r.t.Helper()
	for _, dialect := range []string{"mysql", "postgres"} {
		for _, version := range []string{"02000", "02001"} {
			r.writeMigration(base, dialect, version)
		}
	}
	r.git("add", ".")
	r.git("commit", "--quiet", "-m", "migration baseline")
}

// moveToCurrentPath moves the migrations from base to the current directory,
// as the move out of the initialize package did.
func (r *migrationLineRepo) moveToCurrentPath(base string) {
	r.t.Helper()
	if base == migrationPath {
		return
	}
	if err := os.MkdirAll(filepath.Join(r.dir, filepath.Dir(migrationPath)), 0755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(r.dir, base), filepath.Join(r.dir, migrationPath)); err != nil {
		r.t.Fatal(err)
	}
}

// applyChange edits the working tree: "missing" drops migration 02001 of both
// dialects and "asymmetric" of one; a version adds that migration to both.
func (r *migrationLineRepo) applyChange(change string) {
	r.t.Helper()
	switch change {
	case "":
	case "missing", "asymmetric":
		dialects := []string{"postgres"}
		if change == "missing" {
			dialects = append(dialects, "mysql")
		}
		for _, dialect := range dialects {
			if err := os.Remove(filepath.Join(r.dir, migrationPath, dialect, "02001_fixture.up.sql")); err != nil {
				r.t.Fatal(err)
			}
		}
	default:
		for _, dialect := range []string{"mysql", "postgres"} {
			r.writeMigration(migrationPath, dialect, change)
		}
	}
}

// runGuard runs the guard script against the baseline commit.
func (r *migrationLineRepo) runGuard(script string) (string, error) {
	cmd := exec.Command("bash", script)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "MIGRATION_BASE=HEAD")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestMigrationLineGuardAcrossDirectoryMove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for migration-line fixtures")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required for the migration-line guard")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "script", "check-migration-lines.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, baseline, change, wantError string
	}{
		{"legacy reference", legacyMigrationPath, "", ""},
		{"relocated reference", migrationPath, "", ""},
		{"missing stable migration", legacyMigrationPath, "missing", "exist on HEAD but not here"},
		{"missing dialect", legacyMigrationPath, "asymmetric", "missing one of the two dialects"},
		{"backdated migration", legacyMigrationPath, "01999", "falls below 02001"},
		{"next migration", legacyMigrationPath, "02002", ""},
		{"higher migration", legacyMigrationPath, "03000", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMigrationLineRepo(t)
			repo.commitBaseline(tc.baseline)
			repo.moveToCurrentPath(tc.baseline)
			repo.applyChange(tc.change)
			output, err := repo.runGuard(script)
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("compatible migration lines rejected: %v\n%s", err, output)
				}
			} else if err == nil || !strings.Contains(output, tc.wantError) {
				t.Fatalf("wanted rejection %q, got err=%v\n%s", tc.wantError, err, output)
			}
		})
	}
}
