package initialize

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/google/uuid"
	"github.com/perfect-panel/server/initialize/migrate"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/report"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html
var templateFS embed.FS

var initStatus = make(chan bool)
var configPath string

func Config(path string) (chan bool, *server.Hertz) {
	// Set the configuration file path
	configPath = path
	// get server port
	port := 8080
	host := "127.0.0.1"

	// check gateway mode
	if report.IsGatewayMode() {
		// get free port
		freePort, err := report.ModulePort()
		if err != nil {
			logger.Errorf("get module port error: %s", err.Error())
			panic(err)
		}
		port = freePort
		// register module
		err = report.RegisterModule(port)
		if err != nil {
			logger.Errorf("register module error: %s", err.Error())
			panic(err)
		}
		logger.Infof("module registered on port %d", port)
	}
	engine := newConfigServer(server.WithHostPorts(fmt.Sprintf("%s:%d", host, port)))

	go func() {
		// Start the server
		if err := engine.Run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	return initStatus, engine
}

func newConfigServer(opts ...hertzconfig.Option) *server.Hertz {
	engine := server.Default(opts...)
	engine.SetHTMLTemplate(template.Must(template.ParseFS(templateFS, "templates/*.html")))
	engine.GET("/init", handleInit)
	engine.POST("/init/config", handleInitConfig)
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

func handleInitConfig(_ context.Context, ctx *app.RequestContext) {
	// Load configuration file

	var cfg config.File
	conf.MustLoad(configPath, &cfg)
	var request struct {
		AdminEmail    string `json:"adminEmail"`
		AdminPassword string `json:"adminPassword"`

		DatabaseDriver string `json:"databaseDriver"`
		MysqlHost      string `json:"mysqlHost"`
		MysqlPort      string `json:"mysqlPort"`
		MysqlDatabase  string `json:"mysqlDatabase"`
		MysqlUser      string `json:"mysqlUser"`
		MysqlPassword  string `json:"mysqlPassword"`

		RedisHost     string `json:"redisHost"`
		RedisPort     string `json:"redisPort"`
		RedisPassword string `json:"redisPassword"`
	}
	if err := ctx.BindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{
			"code": 400,
			"msg":  "Invalid request",
			"data": nil,
		})
		ctx.Abort()
		return
	}
	cfg.Debug = false
	// jwt secret
	cfg.JwtAuth.AccessSecret = uuid.New().String()
	// database
	dbConfig, err := buildDatabaseConfig(request.DatabaseDriver, request.MysqlHost, request.MysqlPort, request.MysqlDatabase, request.MysqlUser, request.MysqlPassword)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{
			"code": 400,
			"msg":  err.Error(),
			"data": nil,
		})
		ctx.Abort()
		return
	}
	cfg.SetDatabaseConfig(dbConfig)
	// redis
	cfg.Redis.Host = fmt.Sprintf("%s:%s", request.RedisHost, request.RedisPort)
	cfg.Redis.Pass = request.RedisPassword

	// save config
	fileData, err := yaml.Marshal(cfg)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{
			"code": 500,
			"msg":  "Configuration initialization failed",
			"data": nil,
		})
		ctx.Abort()
		return
	}

	// create database connection
	dbClient := orm.Mysql{Config: dbConfig}
	db, err := orm.ConnectDatabase(dbClient)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{
			"code": 500,
			"msg":  "Database connection failed",
			"data": nil,
		})
		ctx.Abort()
		return
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}
	// migrate database
	if err = migrate.Migrate(dbClient.Driver(), dbClient.MigrationDsn()).Up(); err != nil {
		logger.Errorf("[Init Database] Migrate failed: %v", err.Error())
		ctx.JSON(http.StatusOK, utils.H{
			"code": 500,
			"msg":  "Database migration failed",
			"data": nil,
		})
		ctx.Abort()
		return
	}

	// create admin user
	if err = migrate.CreateAdminUser(request.AdminEmail, request.AdminPassword, db); err != nil {
		logger.Errorf("[Init Database] Create admin user failed: %v", err.Error())
		ctx.JSON(http.StatusOK, utils.H{
			"code": 500,
			"msg":  "Admin user creation failed",
			"data": nil,
		})
		ctx.Abort()
		return
	}

	// write to file
	if err = os.WriteFile(configPath, fileData, 0644); err != nil {
		ctx.JSON(http.StatusInternalServerError, utils.H{
			"code": 500,
			"msg":  "Configuration initialization failed",
			"data": nil,
		})
		ctx.Abort()
		return
	}

	ctx.JSON(http.StatusOK, utils.H{
		"code":   200,
		"msg":    "Configuration initialized",
		"status": true,
	})
	initStatus <- true
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
		ctx.JSON(http.StatusBadRequest, utils.H{
			"code": 400,
			"msg":  "Invalid request",
			"data": nil,
		})
		ctx.Abort()
		return
	}
	var status = true
	var message string
	var tx *sql.DB
	var tables []string
	dbConfig, err := buildDatabaseConfig(request.Driver, request.Host, request.Port, request.Database, request.User, request.Password)
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
	tx, _ = db.DB()
	if tx != nil {
		defer tx.Close()
	}
	if err := tx.Ping(); err != nil {
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

func buildDatabaseConfig(driver, host, port, database, user, password string) (orm.Config, error) {
	normalizedDriver := orm.NormalizeDriver(driver)
	switch normalizedDriver {
	case orm.DriverMySQL, orm.DriverPostgres:
	default:
		return orm.Config{}, fmt.Errorf("unsupported database driver: %s", driver)
	}
	cfg := orm.Config{
		Driver:        normalizedDriver,
		Addr:          fmt.Sprintf("%s:%s", host, port),
		Username:      user,
		Password:      password,
		Dbname:        database,
		MaxIdleConns:  10,
		MaxOpenConns:  10,
		SlowThreshold: orm.DefaultSlowThresholdMs,
	}
	if normalizedDriver == orm.DriverPostgres {
		cfg.Config = orm.DefaultPostgresConfig
	} else {
		cfg.Config = orm.DefaultMySQLConfig
	}
	return cfg, nil
}

func HandleRedisTest(_ context.Context, ctx *app.RequestContext) {
	var request struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		Password string `json:"password"`
	}
	if err := ctx.BindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, utils.H{
			"code": 400,
			"msg":  "Invalid request",
			"data": nil,
		})
		ctx.Abort()
		return
	}
	if err := tool.RedisPing(fmt.Sprintf("%s:%s", request.Host, request.Port), request.Password, 0); err != nil {
		ctx.JSON(http.StatusOK, utils.H{
			"code":   200,
			"msg":    nil,
			"status": false,
		})
		return
	}
	ctx.JSON(http.StatusOK, utils.H{
		"code":   200,
		"msg":    nil,
		"status": true,
	})
}
