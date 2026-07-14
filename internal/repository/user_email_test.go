package repository

import (
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/authmethod"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestQueryAuthMethodsByExactIdentifierEmailUsesCanonicalInput(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			var methods []user.AuthMethods
			stmt := queryAuthMethodsByExactIdentifier(db, authmethod.Email, "alice@example.com").Find(&methods).Statement
			sql := stmt.SQL.String()
			if !strings.Contains(sql, "auth_identifier =") || strings.Contains(sql, "LOWER(") || strings.Contains(sql, "TRIM(") {
				t.Fatalf("email exact query must remain indexed:\n%s", sql)
			}
			if got := stmt.Vars[1]; got != "alice@example.com" {
				t.Fatalf("canonical email query value = %#v, want %q", got, "alice@example.com")
			}
		})
	}
}

func TestQueryFoldedEmailAuthMethodsFallback(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{name: "mysql", dialector: mysql.New(mysql.Config{DSN: "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", SkipInitializeWithVersion: true})},
		{name: "postgres", dialector: postgres.New(postgres.Config{DSN: "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable", PreferSimpleProtocol: true})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			var methods []user.AuthMethods
			stmt := queryFoldedEmailAuthMethods(db, "alice@example.com").Find(&methods).Statement
			sql := stmt.SQL.String()
			if !strings.Contains(sql, "LOWER(TRIM(auth_identifier))") || !strings.Contains(sql, "LIMIT") {
				t.Fatalf("email fallback query must fold and limit:\n%s", sql)
			}
			if got := stmt.Vars[1]; got != "alice@example.com" {
				t.Fatalf("folded email query value = %#v, want %q", got, "alice@example.com")
			}
			if got := stmt.Vars[2]; got != 2 {
				t.Fatalf("folded email query limit = %#v, want 2", got)
			}
		})
	}
}

func TestQueryAuthMethodsByIdentifierNonEmailRemainsExact(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			var method user.AuthMethods
			stmt := queryAuthMethodsByExactIdentifier(db, "google", "OAuth-AbC").First(&method).Statement
			sql := stmt.SQL.String()
			if !strings.Contains(sql, "auth_identifier =") || strings.Contains(sql, "LOWER(") || strings.Contains(sql, "TRIM(") {
				t.Fatalf("non-email query must remain exact:\n%s", sql)
			}
			if got := stmt.Vars[1]; got != "OAuth-AbC" {
				t.Fatalf("non-email query value = %#v, want %q", got, "OAuth-AbC")
			}
		})
	}
}

func TestEmailIdentityCollisionQuery(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{name: "mysql", dialector: mysql.New(mysql.Config{DSN: "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", SkipInitializeWithVersion: true})},
		{name: "postgres", dialector: postgres.New(postgres.Config{DSN: "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable", PreferSimpleProtocol: true})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			var collisions []string
			sql := emailIdentityCollisionQuery(db).Find(&collisions).Statement.SQL.String()
			if !strings.Contains(sql, "LOWER(TRIM(auth_identifier))") || !strings.Contains(sql, "GROUP BY") || !strings.Contains(sql, "HAVING COUNT(*) > 1") || !strings.Contains(sql, "LIMIT") {
				t.Fatalf("collision audit query shape mismatch:\n%s", sql)
			}
		})
	}
}

func TestCanonicalAuthIdentifierRejectsEmptyEmail(t *testing.T) {
	identifier, err := canonicalAuthIdentifier(authmethod.Email, " \t ")
	if !errors.Is(err, ErrInvalidEmailIdentity) {
		t.Fatalf("empty email error = %v, want %v", err, ErrInvalidEmailIdentity)
	}
	if identifier != "" {
		t.Fatalf("empty email identifier = %q, want empty", identifier)
	}

	identifier, err = canonicalAuthIdentifier("google", " OAuth-AbC ")
	if err != nil || identifier != " OAuth-AbC " {
		t.Fatalf("non-email identifier = %q, %v", identifier, err)
	}
}

func TestEmailWriteCollisionQuery(t *testing.T) {
	tests := []struct {
		name      string
		dialector gorm.Dialector
	}{
		{name: "mysql", dialector: mysql.New(mysql.Config{DSN: "gorm:gorm@tcp(localhost:9910)/gorm?charset=utf8&parseTime=True&loc=Local", SkipInitializeWithVersion: true})},
		{name: "postgres", dialector: postgres.New(postgres.Config{DSN: "host=localhost user=gorm password=gorm dbname=gorm port=9920 sslmode=disable", PreferSimpleProtocol: true})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := gorm.Open(tt.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatalf("open gorm db: %v", err)
			}

			var methods []user.AuthMethods
			stmt := emailWriteCollisionQuery(db, "alice@example.com", 7).Find(&methods).Statement
			sql := stmt.SQL.String()
			if !strings.Contains(sql, "LOWER(TRIM(auth_identifier))") || !strings.Contains(sql, "id <>") || !strings.Contains(sql, "LIMIT") {
				t.Fatalf("write collision query shape mismatch:\n%s", sql)
			}
			if got := stmt.Vars[1]; got != "alice@example.com" {
				t.Fatalf("write collision canonical value = %#v, want %q", got, "alice@example.com")
			}
			if got := stmt.Vars[2]; got != int64(7) {
				t.Fatalf("write collision excluded ID = %#v, want 7", got)
			}
		})
	}
}

func TestHasConflictingEmailIdentity(t *testing.T) {
	if hasConflictingEmailIdentity(7, []user.AuthMethods{{Id: 7}}) {
		t.Fatal("current row must not conflict with itself")
	}
	if !hasConflictingEmailIdentity(7, []user.AuthMethods{{Id: 8}}) {
		t.Fatal("different folded-equivalent row must conflict")
	}
}

func TestResolveUniqueAuthMethod(t *testing.T) {
	tests := []struct {
		name    string
		methods []user.AuthMethods
		wantID  int64
		wantErr error
	}{
		{name: "zero", wantErr: gorm.ErrRecordNotFound},
		{name: "one", methods: []user.AuthMethods{{Id: 11}}, wantID: 11},
		{name: "multiple", methods: []user.AuthMethods{{Id: 11}, {Id: 12}}, wantErr: ErrAmbiguousEmailIdentity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, err := resolveUniqueAuthMethod(tt.methods)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("resolveUniqueAuthMethod error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && method.Id != tt.wantID {
				t.Fatalf("resolved ID = %d, want %d", method.Id, tt.wantID)
			}
		})
	}
}
