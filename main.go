package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var name = "galera-health"
var version = "v0.0.1"
var commit = "development"
var date = "0001-01-01T00:00:00.000Z"
var dbPool *sql.DB

var config = &Config{
	Host: "",
	Port: 33060,
	DB: DBConfig{
		Host:     "localhost",
		Port:     3306,
		Username: "monitoring",
		Password: "",
	},
	AvailableWhenDonor: true,
}

const (
	STATE_JOINING        = 1
	STATE_DONOR_DESYNCED = 2
	STATE_JOINED         = 3
	STATE_SYNCED         = 4
)

var flags struct {
	verbose *bool
	host    *string
	port    *int
	config  *string
	version *bool
}

type Config struct {
	Host               string   `yaml:"host"`
	Port               int      `yaml:"port"`
	DB                 DBConfig `yaml:"db" validate:"nonzero"`
	AvailableWhenDonor bool     `yaml:"available_when_donor"`
}

type DBConfig struct {
	Host     string `yaml:"host" validate:"nonzero"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username" validate:"nonzero"`
	Password string `yaml:"password" validate:"nonzero"`
}

func init() {
	flags.verbose = flag.Bool("v", false, "show verbose output")
	flags.host = flag.String("h", "", "IP used for listening, leave empty for all available IP addresses")
	flags.port = flag.Int("p", 33060, "port used for listening")

	flags.config = flag.String("c", "/etc/default/galera-health", "yaml config file")
	flags.version = flag.Bool("V", false, "show version information")
}

func setup() {
	// retrieve flags since that could contain the config folder
	flag.Parse()

	// Set the verbose logging
	if *flags.verbose {
		log.SetLevel(log.DebugLevel)
	}

	if *flags.version {
		log.Infof("%s, %s (%s), build: %s", name, version, date, commit)

		os.Exit(0)
	}

	// Parse the configuration files
	readConfig()

	// Parse the flags
	configFlags()

	// Setup logging
	log.WithFields(log.Fields{
		"host":                 config.Host,
		"port":                 config.Port,
		"db host":              config.DB.Host,
		"db port":              config.DB.Port,
		"db username":          config.DB.Username,
		"available when donor": config.AvailableWhenDonor,
	}).Debug("[setup] config loaded")

	// Initialize DB connection pool
	initDBPool()
}

func initDBPool() {
	connectionString := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/",
		config.DB.Username,
		config.DB.Password,
		config.DB.Host,
		config.DB.Port,
	)

	// Initialise the database connection
	var err error
	dbPool, err = sql.Open("mysql", connectionString)
	if err != nil {
		log.Fatalf("Failed to initialize database connection pool: %v", err)
	}

	// Set database connection pool parameters
	dbPool.SetMaxOpenConns(10)
	dbPool.SetMaxIdleConns(5)
	dbPool.SetConnMaxLifetime(time.Minute * 3)

	// Test database connection with ping
	if err = dbPool.Ping(); err != nil {
		log.Warnf("Could not connect to database: %v", err)
	}
}

func readConfig() {
	log.Debugf("[readConfig] parsing config file: %s", *flags.config)

	// Load YAML config file
	yamlFile, err := os.ReadFile(*flags.config)
	if err != nil {
		log.Fatalf("Could not load config file: %v", err)
	}

	// var yamlConfig *Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Fatalf("Could not parse yaml (%s): %v", *flags.config, err)
	}
}

// command line flags overrule the configuration files
func configFlags() {
	if flags.host != nil && *flags.host != "" {
		config.Host = *flags.host
	}

	if flags.port != nil && *flags.port != 0 {
		config.Port = *flags.port
	}
}

func queryStatus(db *sql.DB, query string) (string, error) {
	var unused, value string
	err := db.QueryRow(query).Scan(&unused, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("variable not found: %s", query)
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func queryStateInt(db *sql.DB, query string) (int, error) {
	var unused string
	var value int
	err := db.QueryRow(query).Scan(&unused, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("variable not found: %s", query)
	}
	if err != nil {
		return 0, err
	}
	return value, nil
}

func checkHealth(db *sql.DB) (bool, string) {
	// Check if galera is enabled
	valueOn, err := queryStatus(db, "SHOW VARIABLES LIKE 'wsrep_on'")
	if err != nil {
		if strings.Contains(err.Error(), "variable not found") {
			log.Warn("[checkHealth] wsrep_on not set")
			return false, "wsrep_on not set"
		}
		return handleError(err)
	}

	// If not a cluster node, it's considered healthy
	if strings.ToLower(valueOn) == "off" {
		return true, "not a cluster node"
	}

	// Check cluster status variables
	valueReady, err := queryStatus(db, "SHOW STATUS LIKE 'wsrep_ready'")
	if err != nil {
		if strings.Contains(err.Error(), "variable not found") {
			log.Warn("[checkHealth] wsrep_ready not set")
			return false, "wsrep_ready not set"
		}
		return handleError(err)
	}

	valueConnected, err := queryStatus(db, "SHOW STATUS LIKE 'wsrep_connected'")
	if err != nil {
		if strings.Contains(err.Error(), "variable not found") {
			log.Warn("[checkHealth] wsrep_connected not set")
			return false, "wsrep_connected not set"
		}
		return handleError(err)
	}

	valueState, err := queryStateInt(db, "SHOW STATUS LIKE 'wsrep_local_state'")
	if err != nil {
		if strings.Contains(err.Error(), "variable not found") {
			log.Warn("[checkHealth] wsrep_local_state not set")
			return false, "wsrep_local_state not set"
		}
		return handleError(err)
	}

	log.WithFields(log.Fields{
		"wsrep_on":          valueOn,
		"wsrep_ready":       valueReady,
		"wsrep_connected":   valueConnected,
		"wsrep_local_state": valueState,
	}).Debug("wsrep status")

	// Check if node is ready
	if strings.ToLower(valueReady) == "off" {
		log.Info("wsrep_ready: off")
		return false, "not ready"
	}

	// Check if node is connected to cluster
	if strings.ToLower(valueConnected) == "off" {
		log.Info("wsrep_connected: off")
		return false, "not connected"
	}

	// Check node state
	switch valueState {
	case STATE_JOINING:
		log.Info("wsrep_local_state: joining")
		return false, "joining"
	case STATE_DONOR_DESYNCED:
		log.Info("wsrep_local_state: donor")
		if config.AvailableWhenDonor {
			return true, "donor"
		}
		return false, "donor"
	case STATE_JOINED:
		log.Info("wsrep_local_state: joined")
		return false, "joined"
	case STATE_SYNCED:
		log.Debug("wsrep_local_state: synced")
		return true, "synced"
	default:
		log.Warnf("wsrep_local_state: Unrecognized state: %d", valueState)
		return false, fmt.Sprintf("unrecognized state: %d", valueState)
	}
}

func handleError(err error) (bool, string) {
	log.Errorf("[query error] %s", err.Error())

	if strings.Contains(err.Error(), "connection refused") {
		return false, "connection refused"
	}
	return false, err.Error()
}

func healthcheck(w http.ResponseWriter, _ *http.Request) {
	var statusCode int
	var message string

	// Check database connection
	if err := dbPool.Ping(); err != nil {
		log.Errorf("Database connection error: %v", err)
		statusCode = http.StatusServiceUnavailable
		message = "database connection error"
	} else {
		// Check cluster health
		healthy, healthMessage := checkHealth(dbPool)
		message = healthMessage

		if healthy {
			statusCode = http.StatusOK
		} else {
			statusCode = http.StatusServiceUnavailable
		}
	}

	// Log health check response
	log.WithFields(log.Fields{
		"statusCode": statusCode,
		"message":    message,
	}).Debug("Health check response")

	w.WriteHeader(statusCode)
	if _, err := w.Write([]byte(message)); err != nil {
		log.Errorf("Failed to write response: %v", err)
	}
}

func main() {
	setup()

	// Configure HTTP server with timeouts
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      http.DefaultServeMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Register handler
	http.HandleFunc("/", healthcheck)

	// Start server in a goroutine
	go func() {
		log.Info("Started on ", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Could not start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("Shutting down server...")

	// Create a deadline for the shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Close database connection pool
	if dbPool != nil {
		if err := dbPool.Close(); err != nil {
			log.Errorf("Error closing database connection pool: %v", err)
		}
	}

	// Gracefully shutdown the server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Info("Server gracefully stopped")
}
