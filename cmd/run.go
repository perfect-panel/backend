package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"uuid"

	"github.com/perfect-panel/backend/internal/app"
	"github.com/perfect-panel/backend/internal/app/buildinfo"
	"github.com/perfect-panel/backend/internal/app/lifecycle"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/transport/http/setup"
	"github.com/perfect-panel/backend/pkg/conf"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	startCmd.Flags().StringVar(&startConfigPath, "config", "etc/ppanel.yaml", "ppanel.yaml directory to read from")
}

var (
	startConfigPath string
)

var startCmd = &cobra.Command{
	Use:   "run",
	Short: "start PPanel",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("[PPanel version] " + buildinfo.Display())
		run(cmd.Context())
	},
}

// run starts the servers and stops them when the process receives SIGINT,
// SIGTERM or SIGQUIT. ctx is the command's root context: start-up serves no
// request, so nothing narrower exists.
func run(ctx context.Context) {
	services := getServers(ctx)
	defer services.Stop()
	go services.Start()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-quit
}

// getServers loads the configuration, running the setup wizard first when it
// is incomplete, prepares the process (clock zone, logger) and builds the
// servers from it.
func getServers(ctx context.Context) *lifecycle.Group {
	var c config.Config
	createConfigFileIfMissing()
	// The wizard writes the file and reports once it is complete, or with
	// the error that stopped its listener.
	if initConfig(&c) {
		status, engine := setup.Start(startConfigPath, c.Port)
		if err := <-status; err != nil {
			log.Fatalf("setup wizard: %v", err)
		}
		if err := engine.Shutdown(ctx); err != nil {
			log.Printf("Init Server Shutdown: %s\n", err.Error())
		}
	}
	conf.MustLoad(startConfigPath, &c)
	if err := c.Redis.Validate(); err != nil {
		log.Fatal(err)
	}
	// Sessions, order event tickets and guest-checkout HMACs are all keyed by
	// this secret; an empty one lets anyone forge them.
	if c.JwtAuth.AccessSecret == "" {
		log.Fatalf("JwtAuth.AccessSecret is empty in %s; set a long random secret", startConfigPath)
	}
	if len(c.JwtAuth.AccessSecret) < 16 {
		log.Printf("warning: JwtAuth.AccessSecret in %s is shorter than 16 characters and can be guessed; replace it with a long random secret", startConfigPath)
	}
	// The application timezone is the process's single clock: business times,
	// GORM's timestamps and the database session all use it.
	if err := timeutil.LoadProcessLocation(c.AppLocation); err != nil {
		logger.Errorf("load app timezone %q failed: %v, falling back to Local", c.AppLocation, err)
	}
	if err := logger.SetUp(c.Logger); err != nil {
		logger.Errorf("Logger setup failed: %v", err.Error())
	}

	return app.NewServices(c)
}

// createConfigFileIfMissing creates an empty configuration file, and its
// directory, when the file does not exist, so that initConfig starts the
// setup wizard on it. It ends the process when either cannot be created.
func createConfigFileIfMissing() {
	if _, err := os.Stat(startConfigPath); !os.IsNotExist(err) {
		return
	}
	dir := filepath.Dir(startConfigPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Fatalf("Please create the directory %s and place the configuration file %s in it: %v", dir, startConfigPath, err)
	}
	// The file will hold the JWT secret and database credentials.
	file, err := os.OpenFile(startConfigPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		logger.Errorf("Please create the configuration file %s in the directory %s.\n", startConfigPath, dir)
		panic(fmt.Sprintf("Please create the configuration file %s in the directory %s: %v\n", startConfigPath, dir, err))
	}
	// Nothing was written, so a failed close loses nothing; the wizard writes
	// the file through its own handle.
	_ = file.Close()
}

// initConfig loads the configuration file into c and reports whether the
// setup wizard has to run first. The decision follows the file's content,
// not its path: a file that names a database is complete (one without a JWT
// secret then ends the start in getServers), and a file without one is a
// new installation, which the PPANEL_DB and PPANEL_REDIS environment
// variables complete — the configuration gets a generated secret, unless
// the file has one, and those connections, and is written back — and the
// wizard otherwise. The path used to decide: only the default etc/ppanel.yaml
// took the environment, so a unit started with --config /opt/.../ppanel.yaml
// silently ran the wizard instead.
func initConfig(c *config.Config) bool {
	conf.MustLoad(startConfigPath, c)
	if c.DatabaseConfig().Addr != "" {
		return false
	}
	if c.JwtAuth.AccessSecret == "" {
		c.JwtAuth.AccessSecret = uuid.NewV4().String()
	}
	dsn := os.Getenv("PPANEL_DB")
	if dsn == "" {
		return true
	}
	// The session zone of the new database follows the file's AppLocation:
	// stored times and daily statistics are read in the session zone.
	cfg := orm.ParseDSNIn(dsn, c.AppLocation)
	if cfg == nil {
		log.Printf("PPANEL_DB is not a database DSN; starting the setup wizard instead")
		return true
	}
	c.SetDatabaseConfig(*cfg)

	uri := os.Getenv("PPANEL_REDIS")
	if uri == "" {
		return true
	}
	rds, err := c.Redis.WithURI(uri)
	if err != nil {
		log.Printf("PPANEL_REDIS is not a redis:// URI (%v); starting the setup wizard instead", err)
		return true
	}
	c.Redis = rds

	// Every boot setting the file already had is written back with the
	// generated secret and the connections.
	newConfig := config.File{
		Host:           c.Host,
		AppLocation:    c.AppLocation,
		Port:           c.Port,
		Transport:      c.Transport,
		TLS:            c.TLS,
		Debug:          c.Debug,
		TrustedProxies: c.TrustedProxies,
		AllowedOrigins: c.AllowedOrigins,
		HTTP:           c.HTTP,
		GeoIP:          c.GeoIP,
		JwtAuth:        c.JwtAuth,
		Logger:         c.Logger,
		Trace:          c.Trace,
		Database:       c.DatabaseConfig(),
		Redis:          c.Redis,
		EdgeSubscribe:  c.EdgeSubscribe,
		Administrator:  c.Administrator,
	}
	fileData, err := yaml.Marshal(newConfig)
	if err != nil {
		log.Fatalf("encode the configuration: %v", err)
	}
	// The file holds the JWT secret and database credentials, and WriteFile
	// keeps the mode of an existing file.
	if err := os.WriteFile(startConfigPath, fileData, 0600); err != nil {
		log.Fatalf("write %s: %v", startConfigPath, err)
	}
	if err := os.Chmod(startConfigPath, 0600); err != nil {
		log.Fatalf("restrict %s to its owner: %v", startConfigPath, err)
	}
	return false
}
