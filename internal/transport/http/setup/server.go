// Package setup serves the first-installation wizard. While the server has no
// usable configuration, the command line serves this page on the loopback
// interface, on the configured port, instead of the API. The page tests the
// database and Redis the installer enters; on submit the wizard checks the
// input and that the database is empty, writes the configuration file,
// migrates the database and creates the first administrator, then signals
// the caller to start the real server. The file is written before the
// migration, so a migration that fails leaves an installation the next start
// resumes (the bootstrap migrates and seeds the administrator named in the
// file) instead of a database the wizard refuses as already used.
package setup

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/perfect-panel/server/internal/app/migration/schema"
	"github.com/perfect-panel/server/internal/auth/identifier"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/orm"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

//go:embed templates/*.html
var templateFS embed.FS

// MinAdminPasswordLength is the shortest administrator password the wizard
// accepts; the page enforces the same, the handler does not trust it.
const MinAdminPasswordLength = 8

// DefaultPort is the port the wizard listens on when the configuration
// names none.
const DefaultPort = 8080

// wizard is one installation: the configuration file it writes, the signal
// to the caller and the guard against two submissions running at once.
type wizard struct {
	configPath string
	// done receives nil once the installation is complete, or the error
	// that stopped the listener. Buffered, so the sender never blocks.
	done chan error

	mu         sync.Mutex
	installing bool
	installed  bool

	// The database steps, replaceable by tests.
	connect     func(orm.Config) (*gorm.DB, error)
	migrate     func(driver, dsn string) error
	createAdmin func(email, password string, db *gorm.DB) error
}

func newWizard(configPath string) *wizard {
	return &wizard{
		configPath:  configPath,
		done:        make(chan error, 1),
		connect:     func(cfg orm.Config) (*gorm.DB, error) { return orm.ConnectDatabase(orm.Mysql{Config: cfg}) },
		migrate:     schema.Up,
		createAdmin: schema.CreateAdminUser,
	}
}

// Start serves the first-installation UI on 127.0.0.1 and port (DefaultPort
// when zero). The channel receives nil once the installation is complete
// and the caller can start the real server, or the error that stopped the
// listener; the caller decides what to do about that, so no goroutine here
// ends the process.
func Start(path string, port int) (<-chan error, *server.Hertz) {
	w := newWizard(path)
	addr := Address(port)
	engine := newConfigServer(w, server.WithHostPorts(addr))

	go func() {
		defer func() {
			// Hertz's netpoll transport panics when the listener cannot
			// bind.
			if r := recover(); r != nil {
				w.fail(fmt.Errorf("listen on %s: %v", addr, r))
			}
		}()
		if err := engine.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.fail(fmt.Errorf("listen on %s: %w", addr, err))
		}
	}()
	logger.Infof("[Setup] the installation wizard is at http://%s/init; on a remote host reach it through an SSH tunnel (ssh -L %d:127.0.0.1:%d <host>) or install with PPANEL_DB and PPANEL_REDIS", addr, listenPort(port), listenPort(port))
	return w.done, engine
}

// Address is the wizard's listen address for port: the loopback interface,
// so the unauthenticated installer is reachable from the host only.
func Address(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort(port)))
}

func listenPort(port int) int {
	if port <= 0 {
		return DefaultPort
	}
	return port
}

func newConfigServer(w *wizard, opts ...hertzconfig.Option) *server.Hertz {
	engine := server.Default(opts...)
	engine.SetHTMLTemplate(template.Must(template.ParseFS(templateFS, "templates/*.html")))
	engine.GET("/init", handleInit)
	engine.POST("/init/config", w.handleInitConfig)
	engine.POST("/init/database/test", HandleDatabaseTest)
	engine.POST("/init/mysql/test", HandleMySQLTest)
	engine.POST("/init/redis/test", HandleRedisTest)
	engine.NoRoute(func(_ context.Context, ctx *app.RequestContext) {
		ctx.Redirect(http.StatusFound, []byte("/init"))
	})
	return engine
}

func handleInit(_ context.Context, ctx *app.RequestContext) {
	ctx.HTML(http.StatusOK, "index.html", nil)
}

// initRequest is the installer's form.
type initRequest struct {
	AdminEmail    string `json:"adminEmail"`
	AdminPassword string `json:"adminPassword"`

	DatabaseDriver string `json:"databaseDriver"`
	MysqlHost      string `json:"mysqlHost"`
	MysqlPort      string `json:"mysqlPort"`
	MysqlDatabase  string `json:"mysqlDatabase"`
	MysqlUser      string `json:"mysqlUser"`
	MysqlPassword  string `json:"mysqlPassword"`

	RedisHost      string `json:"redisHost"`
	RedisPort      string `json:"redisPort"`
	RedisPassword  string `json:"redisPassword"`
	RedisUsername  string `json:"redisUsername"`
	RedisKeyPrefix string `json:"redisKeyPrefix"`
}

// validate checks the administrator account the page also validates — the
// handler does not trust the page — and returns the canonical email and the
// complaint to show the installer, "" when the account is acceptable.
func (r initRequest) validate() (email, complaint string) {
	email, err := identifier.ValidateEmail(r.AdminEmail, "", false)
	if err != nil {
		return "", "Administrator email is not a valid email address"
	}
	if utf8.RuneCountInString(r.AdminPassword) < MinAdminPasswordLength {
		return "", fmt.Sprintf("Password must be at least %d characters", MinAdminPasswordLength)
	}
	if err := (config.RedisConfig{KeyPrefix: r.RedisKeyPrefix}).Validate(); err != nil {
		return "", err.Error()
	}
	return email, ""
}

// begin claims the installation for one request; it is false while another
// request installs or once the installation is complete.
func (w *wizard) begin() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.installing || w.installed {
		return false
	}
	w.installing = true
	return true
}

// end releases the installation; a successful one signals the caller.
func (w *wizard) end(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.installing = false
	if success {
		w.installed = true
		w.signal(nil)
	}
}

// fail reports the error that stopped the listener.
func (w *wizard) fail(err error) {
	w.signal(err)
}

func (w *wizard) signal(err error) {
	select {
	case w.done <- err:
	default:
	}
}

func reply(ctx *app.RequestContext, status int, msg string) {
	ctx.JSON(status, utils.H{
		"code": status,
		"msg":  msg,
		"data": nil,
	})
	ctx.Abort()
}

func (w *wizard) handleInitConfig(_ context.Context, ctx *app.RequestContext) {
	var request initRequest
	if err := ctx.BindJSON(&request); err != nil {
		reply(ctx, http.StatusBadRequest, "Invalid request")
		return
	}
	adminEmail, complaint := request.validate()
	if complaint != "" {
		reply(ctx, http.StatusBadRequest, complaint)
		return
	}
	if !w.begin() {
		reply(ctx, http.StatusConflict, "An installation is already in progress or complete")
		return
	}
	success := false
	defer func() { w.end(success) }()

	var cfg config.File
	if err := conf.Load(w.configPath, &cfg); err != nil {
		logger.Errorf("[Init Config] read %s: %v", w.configPath, err)
		reply(ctx, http.StatusInternalServerError, "Configuration file could not be read")
		return
	}
	cfg.Debug = false
	// jwt secret
	cfg.JwtAuth.AccessSecret = uuid.NewV4().String()
	// database
	dbConfig, err := buildDatabaseConfig(request.DatabaseDriver, request.MysqlHost, request.MysqlPort, request.MysqlDatabase, request.MysqlUser, request.MysqlPassword, cfg.AppLocation)
	if err != nil {
		reply(ctx, http.StatusBadRequest, err.Error())
		return
	}
	cfg.SetDatabaseConfig(dbConfig)
	// redis
	cfg.Redis.Host = net.JoinHostPort(request.RedisHost, request.RedisPort)
	cfg.Redis.Pass = request.RedisPassword
	cfg.Redis.Username = request.RedisUsername
	cfg.Redis.KeyPrefix = request.RedisKeyPrefix
	// The administrator the start seeds when the wizard's own seed below
	// does not run to completion; the password is not written anywhere.
	cfg.Administrator.Email = adminEmail
	cfg.Administrator.Password = ""

	fileData, err := yaml.Marshal(cfg)
	if err != nil {
		reply(ctx, http.StatusInternalServerError, "Configuration initialization failed")
		return
	}

	// create database connection
	db, err := w.connect(dbConfig)
	if err != nil {
		logger.Errorf("[Init Database] connect failed: %v", err.Error())
		reply(ctx, http.StatusInternalServerError, "Database connection failed")
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		defer closeDatabase(sqlDB)
	}
	// The connection test can be skipped by posting straight to this
	// endpoint, so the database is checked here too: another panel's data
	// must not be migrated over.
	tables, err := db.Migrator().GetTables()
	if err != nil {
		logger.Errorf("[Init Database] table check failed: %v", err.Error())
		reply(ctx, http.StatusInternalServerError, "Database table check failed")
		return
	}
	if len(tables) > 0 {
		reply(ctx, http.StatusBadRequest, "The database contains existing data. Please clear it before proceeding with the installation.")
		return
	}

	// The file first: it holds the JWT secret and database credentials, and
	// WriteFile keeps the mode of an existing file. From here on a failure
	// leaves an installation the next start resumes.
	if err = os.WriteFile(w.configPath, fileData, 0600); err == nil {
		err = os.Chmod(w.configPath, 0600)
	}
	if err != nil {
		logger.Errorf("[Init Config] write %s: %v", w.configPath, err)
		reply(ctx, http.StatusInternalServerError, "Configuration initialization failed")
		return
	}

	dbClient := orm.Mysql{Config: dbConfig}
	if err = w.migrate(dbClient.Driver(), dbClient.MigrationDsn()); err != nil {
		logger.Errorf("[Init Database] Migrate failed: %v", err.Error())
		reply(ctx, http.StatusInternalServerError, "Database migration failed. The configuration was saved: fix the database (or drop and recreate it) and start the server again to resume the installation; the administrator's password is then printed once in the log.")
		return
	}

	if err = w.createAdmin(adminEmail, request.AdminPassword, db); err != nil {
		logger.Errorf("[Init Database] Create admin user failed: %v", err.Error())
		reply(ctx, http.StatusInternalServerError, "Admin user creation failed. The configuration was saved: start the server again to resume the installation; the administrator's password is then printed once in the log.")
		return
	}

	success = true
	ctx.JSON(http.StatusOK, utils.H{
		"code":   200,
		"msg":    "Configuration initialized",
		"status": true,
	})
}

func HandleMySQLTest(ctx context.Context, requestCtx *app.RequestContext) {
	HandleDatabaseTest(ctx, requestCtx)
}

func HandleDatabaseTest(_ context.Context, ctx *app.RequestContext) {
	var request struct {
		Driver   string `json:"driver"`
		Host     string `json:"host"`
		Port     string `json:"port"`
		Database string `json:"database"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := ctx.BindJSON(&request); err != nil {
		reply(ctx, http.StatusBadRequest, "Invalid request")
		return
	}
	var status = true
	var message string
	var tx *sql.DB
	var tables []string
	// Only the connection is tested; the session zone does not matter.
	dbConfig, err := buildDatabaseConfig(request.Driver, request.Host, request.Port, request.Database, request.User, request.Password, orm.DefaultLocation)
	if err != nil {
		ctx.JSON(http.StatusOK, utils.H{
			"code":   200,
			"msg":    err.Error(),
			"status": false,
		})
		return
	}
	db, err := orm.ConnectDatabase(orm.Mysql{Config: dbConfig})
	if err != nil {
		logger.Errorf("connect database failed, err: %v\n", err.Error())
		status = false
		message = "Database connection failed"
		goto result
	}
	tx, err = db.DB()
	if err != nil {
		logger.Errorf("get database connection failed, err: %v\n", err.Error())
		status = false
		message = "Database connection failed"
		goto result
	}
	defer closeDatabase(tx)
	if err := pingDatabase(tx); err != nil {
		logger.Errorf("ping database failed, err: %v\n", err.Error())
		status = false
		message = "Database connection failed"
	}

	tables, err = db.Migrator().GetTables()
	if err != nil {
		logger.Errorf("database table check failed, err: %v\n", err.Error())
		status = false
		message = "Database table check failed"
		goto result
	}
	if len(tables) > 0 {
		status = false
		message = "The database contains existing data. Please clear it before proceeding with the installation."
		goto result
	}

result:
	ctx.JSON(http.StatusOK, utils.H{
		"code":   200,
		"msg":    message,
		"status": status,
	})
}

// pingDatabase checks that the connection answers within a bounded time, so
// an address that drops packets cannot hold the install page indefinitely.
func pingDatabase(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

// closeDatabase releases the connection pool a setup request opened. The
// response is already decided by then, so a failure is only logged.
func closeDatabase(db io.Closer) {
	if err := db.Close(); err != nil {
		logger.Errorf("[Init Database] close database connection failed: %v", err.Error())
	}
}

// buildDatabaseConfig is the configuration of the database the installation
// sets up. Its session stores times in zone, the application's zone.
func buildDatabaseConfig(driver, host, port, database, user, password, zone string) (orm.Config, error) {
	normalizedDriver := orm.NormalizeDriver(driver)
	switch normalizedDriver {
	case orm.DriverMySQL, orm.DriverPostgres:
	default:
		return orm.Config{}, fmt.Errorf("unsupported database driver: %s", driver)
	}
	cfg := orm.Config{
		Driver:          normalizedDriver,
		Addr:            fmt.Sprintf("%s:%s", host, port),
		Username:        user,
		Password:        password,
		Dbname:          database,
		MaxIdleConns:    10,
		MaxOpenConns:    10,
		ConnMaxLifetime: orm.DefaultConnMaxLifetimeSeconds,
		ConnMaxIdleTime: orm.DefaultConnMaxIdleTimeSeconds,
		SlowThreshold:   orm.DefaultSlowThresholdMs,
	}
	// A new database stores times in the application's zone from the
	// start. The parameters are written out, so a later AppLocation change
	// cannot silently reinterpret the stored times.
	if normalizedDriver == orm.DriverPostgres {
		cfg.Config = orm.DefaultPostgresQuery(zone)
	} else {
		cfg.Config = orm.DefaultMySQLQuery(zone)
	}
	return cfg, nil
}

// redisTestTimeout bounds the Redis connection test. Without it an address
// that drops packets keeps the install page waiting through every dial retry
// of the client.
const redisTestTimeout = 5 * time.Second

// HandleRedisTest reports whether the Redis server the installer entered
// answers, before the configuration is written.
func HandleRedisTest(ctx context.Context, requestCtx *app.RequestContext) {
	var request struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		Password string `json:"password"`
		Username string `json:"username"`
	}
	if err := requestCtx.BindJSON(&request); err != nil {
		reply(requestCtx, http.StatusBadRequest, "Invalid request")
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, redisTestTimeout)
	defer cancel()
	rds := config.RedisConfig{Host: net.JoinHostPort(request.Host, request.Port), Username: request.Username, Pass: request.Password}
	if err := rds.Ping(pingCtx); err != nil {
		requestCtx.JSON(http.StatusOK, utils.H{
			"code":   200,
			"msg":    nil,
			"status": false,
		})
		return
	}
	requestCtx.JSON(http.StatusOK, utils.H{
		"code":   200,
		"msg":    nil,
		"status": true,
	})
}
