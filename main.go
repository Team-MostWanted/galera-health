package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var name = "galera-health"
var version = "v0.0.1"
var commit = "development"
var date = "0001-01-01T00:00:00.000Z"

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

	// parse the configuration files
	readConfig()

	// parse the flags
	configFlags()

	log.WithFields(log.Fields{
		"host":                 config.Host,
		"port":                 config.Port,
		"db host":              config.DB.Host,
		"db port":              config.DB.Port,
		"db username":          config.DB.Username,
		"available when donor": config.AvailableWhenDonor,
	}).Debug("[setup] config loaded")
}

func readConfig() {
	log.Debugf("[readConfig] parsing config file: %s", *flags.config)

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

func checkHealth(db *sql.DB) (bool, string) {
	var unused string
	var valueOn string
	var valueReady string
	var valueConnected string
	var valueState int

	errOn := db.QueryRow("SHOW VARIABLES LIKE 'wsrep_on'").Scan(&unused, &valueOn)
	errReady := db.QueryRow("SHOW STATUS LIKE 'wsrep_ready'").Scan(&unused, &valueReady)
	errConnected := db.QueryRow("SHOW STATUS LIKE 'wsrep_connected'").Scan(&unused, &valueConnected)
	errState := db.QueryRow("SHOW STATUS LIKE 'wsrep_local_state'").Scan(&unused, &valueState)

	if errors.Is(errOn, sql.ErrNoRows) {
		log.Warn("[checkHealth] wsrep_on not set")
		log.Debugf("[checkHealth] errOn: %v", errOn)

		return false, "wsrep_on not set"
	} else if errOn != nil {
		return handleError(errOn)
	}

	if strings.Compare(strings.ToLower(valueOn), "off") == 0 {
		return true, "not a cluster node"
	}

	if errors.Is(errReady, sql.ErrNoRows) || errors.Is(errConnected, sql.ErrNoRows) || errors.Is(errState, sql.ErrNoRows) {
		log.Warn("[checkHealth] required variables not set")
		log.Debugf("[checkHealth] errReady: %v", errReady)
		log.Debugf("[checkHealth] errConnected: %v", errConnected)
		log.Debugf("[checkHealth] errState: %v", errState)

		return false, "required variables not set"
	} else if errReady != nil {
		return handleError(errReady)
	} else if errConnected != nil {
		return handleError(errConnected)
	} else if errState != nil {
		return handleError(errState)
	}

	log.WithFields(log.Fields{
		"wsrep_on":          valueOn,
		"wsrep_ready":       valueReady,
		"wsrep_connected":   valueConnected,
		"wsrep_local_state": valueState,
	}).Debug("wsrep status")

	if strings.Compare(strings.ToLower(valueReady), "off") == 0 {
		log.Infof("wsrep_ready: %s", valueReady)

		return false, "not ready"
	}

	if strings.Compare(strings.ToLower(valueConnected), "off") == 0 {
		log.Infof("wsrep_connected: %s", valueConnected)

		return false, "not connected"
	}

	switch valueState {
	case STATE_JOINING:
		log.Info("wsrep_local_state: joining")

		return false, "joining"
	case STATE_DONOR_DESYNCED:
		if config.AvailableWhenDonor {
			return true, "donor"
		}

		log.Info("wsrep_local_state: donor")

		return false, "donor"
	case STATE_JOINED:
		log.Info("wsrep_local_state: joined")

		return false, "joined"
	case STATE_SYNCED:
		return true, "synced"
	default:
		log.Infof("wsrep_local_state: Unrecognized state: %d", valueState)

		return false, fmt.Sprintf("Unrecognized state: %d", valueState)
	}
}

func handleError(err error) (bool, string) {
	log.Errorf("[query error] %s", err.Error())

	if strings.Contains(err.Error(), "connection refused") {
		return false, "connection refused"
	} else {
		return false, err.Error()
	}
}

func healthcheck(w http.ResponseWriter, _ *http.Request) {
	var connectionString = fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/",
		config.DB.Username,
		config.DB.Password,
		config.DB.Host,
		config.DB.Port,
	)

	db, err := sql.Open(
		"mysql",
		connectionString,
	)

	var statusCode int
	var responseBody string

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			log.Fatalf("[healthcheck] close db connection error: %v", err)
		}
	}(db)

	if err != nil {
		statusCode = http.StatusServiceUnavailable
		responseBody = err.Error()
	} else {
		healthy, message := checkHealth(db)

		if healthy {
			statusCode = http.StatusOK
			responseBody = message
		} else {
			statusCode = http.StatusServiceUnavailable
			responseBody = message
		}
	}

	log.Debugf("statusCode: %d", statusCode)
	log.Debugf("responseBody: %s", responseBody)

	w.WriteHeader(statusCode)
	_, err = w.Write([]byte(responseBody))
	if err != nil {
		return
	}
}

func main() {
	setup()

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)

	log.Info("Started on ", addr)

	http.HandleFunc("/", healthcheck)
	err := http.ListenAndServe(addr, nil)

	if err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}
