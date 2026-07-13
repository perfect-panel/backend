package cmd

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/pkg/constant"

	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/service"
	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/queue"
	"github.com/perfect-panel/server/scheduler"
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
		fmt.Println("[PPanel version] v" + fmt.Sprintf("%s(%s)", constant.Version, constant.BuildTime))
		run()
	},
}

func run() {
	services := getServers()
	defer services.Stop()
	go services.Start()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-quit
}
func getServers() *service.Group {
	var c config.Config

	// check config file is exist
	if _, err := os.Stat(startConfigPath); os.IsNotExist(err) {
		// check directory is existed
		if _, err := os.Stat("etc"); os.IsNotExist(err) {
			logger.Errorf("Directory %s does not exist. Creating it...\n", "etc")
			if err = os.MkdirAll("etc", os.ModePerm); err != nil {
				log.Fatalf("Please create the directory %s and place the configuration file %s in it.\n", "etc", startConfigPath)
			}
		}
		// create new config file
		if _, err := os.Create(startConfigPath); err != nil {
			logger.Errorf("Please create the configuration file %s in the directory %s.\n", startConfigPath, "etc")
			panic(fmt.Sprintf("Please create the configuration file %s in the directory %s.\n", startConfigPath, "etc"))
		}
	}
	// check config file is empty, if empty, start init web server
	if initConfig(&c) {
		status, engine := initialize.Config(startConfigPath)
		<-status
		if err := engine.Shutdown(context.TODO()); err != nil {
			log.Printf("Init Server Shutdown: %s\n", err.Error())
		}
	}
	conf.MustLoad(startConfigPath, &c)
	// Initialize application timezone
	if err := timeutil.LoadLocation(c.AppLocation); err != nil {
		logger.Errorf("load app timezone %q failed: %v, falling back to Local", c.AppLocation, err)
	}
	// init logger
	if err := logger.SetUp(c.Logger); err != nil {
		logger.Errorf("Logger setup failed: %v", err.Error())
	}

	// init service context
	ctx := svc.NewServiceContext(c)

	// init plugin manager
	pluginHostEnv := &plugin.HostEnv{
		Config: c,
		Redis:  plugin.NewRedisAdapter(ctx.Redis),
		Store:  plugin.NewStoreAdapter(ctx.Store.DB()),
		Queue:  plugin.NewQueueAdapter(ctx.Queue),
	}
	pluginMgr := plugin.NewManager(pluginHostEnv)
	ctx.PluginReady = pluginMgr
	ctx.PluginMgr = pluginMgr

	services := service.NewServiceGroup()
	services.Add(internal.NewService(ctx))
	services.Add(queue.NewService(ctx))
	services.Add(scheduler.NewService(ctx))
	services.Add(pluginMgr)
	return services
}

func initConfig(c *config.Config) bool {
	// load config
	conf.MustLoad(startConfigPath, c)
	//  check custom config
	if startConfigPath != "etc/ppanel.yaml" && c.DatabaseConfig().Addr == "" {
		return true
	}
	// check access secret
	if c.JwtAuth.AccessSecret == "" && startConfigPath == "etc/ppanel.yaml" {
		c.JwtAuth.AccessSecret = uuid.New().String()
		// Get environment variables
		dsn := os.Getenv("PPANEL_DB")
		if dsn == "" {
			return true
		}
		cfg := orm.ParseDSN(dsn)
		if cfg == nil {
			return true
		} else {
			c.SetDatabaseConfig(*cfg)
		}

		// Get environment variables
		uri := os.Getenv("PPANEL_REDIS")
		if uri == "" {
			return true
		}
		addr, pass, db, err := tool.ParseRedisURI(uri)
		if err != nil {
			return true
		} else {
			c.Redis.Host = addr
			c.Redis.Pass = pass
			c.Redis.DB = db
		}
		// save yaml file
		newConfig := config.File{
			Host:     c.Host,
			Port:     c.Port,
			Debug:    c.Debug,
			JwtAuth:  c.JwtAuth,
			Logger:   c.Logger,
			Database: c.DatabaseConfig(),
			Redis:    c.Redis,
		}
		fileData, err := yaml.Marshal(newConfig)
		if err != nil {
			panic(err.Error())
		}
		// write to file
		if err := os.WriteFile(startConfigPath, fileData, 0644); err != nil {
			panic(err.Error())
		}
	}
	return false
}
