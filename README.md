# Galera Health Check

HTTP Server tha reports the health status of a galera cluster nocde

Server settings can be configured via [parameters](#Parameters) or via [configuration](#Configuration). Parameters will take precedence over configuration settings.

Database settings can only be configured via [configuration](#Configuration).

## Usage

Create an appropriate [configuration](#Configuration).

To Start:
```
./galera-health
```

The output given should look like:

```
INFO[0000] Started on :33060
```

### Parameters

The script exporter has the following command line arguments:

```
Usage of script_exporter
  -V	show version information
  -c string
    	config file in yaml format (default "/etc/default/galera-health")
  -h string
    	ip used for listening, leave empty for all available IP addresses
  -p int
    	port used for listening (default 33060)
  -v	show verbose output
```

The -h and -p setting will overrule the settings made in the config files for these specific options.

### Configuration
The configuration files are made in yaml format.

Default the config files is `/etc/default/galera-health`.

key                  | description
---------------------|----------
host                 | ip used for listening, leave empty for all available IP addresses
port                 | port used for listening (default 33060)
db                   | database config options
available_when_donor | should being a donor be considered health or unhealth, when set to true donors are concidered health (default true)

Database sub options

key      | description
---------|----------
host     | ip address or hostname of the database server (default localhost)
port     | port to use when connecting to the database server (default 3306)
username | useranem to connect to the database (default monitoring)
password | password for the user to connect to the database

Example:

```
---
host: 127.0.0.1
port: 80
db:
    host: localhost
    port: 3306
    username: root
    password: TopSecret!
available_when_donor: true
```

### Endpoints

This server exposes a single GET endpoint on /

#### HTTP Results

HTTP 200 OK is returned when the database server is healthy
HTTP 503 Service Unavailable is returned when the server is unhealth

THe result body displays additional information on the result

##### HTTP 200 OK messages

message      | description
-----------------------|----------
not a cluster node     | server is health, but not a galera cluster node
donor                  | returned when wsrep_local_state equals `2` and `available_when_donor` is configured to `true`
synced                 | returned when wsrep_local_state equals `4`

##### HTTP 503 Service Unavailable messages

message      | description
--------------------------------|----------
wsrep_on not set                | variable `wsrep_on` is not set on the server
required variables not set      | status wsrep_ready, status wsrep_connected or status wsrep_local_state are not set on the server, this should not happen when wsrep_on = On
not ready                       | returned when wsrep_ready equals `off`
not connected                   | returned when wsrep_connected equals `off`
joining                         | returned when wsrep_local_state equals `1`
donor                           | returned when wsrep_local_state equals `2` and `available_when_donor` is configured to `false`
joined                          | returned when wsrep_local_state equals `3`
Unrecognized state: {str}       | returned when wsrep_local_state anything other than 1, 2, 3 or 4
connection refused              | returned when no connection to the configured database server could be made
{error message}                 | returned when an error occurs that is not otherwise defined

## Developing

If you want to contribute to this project please follow these guidelines:

- Script Exporter is build in [Golang](https://golang.org/)
- Use style guides as described in [.editorconfig](.editorconfig)
- Changes in features should be reflected in this [README.md](README.md)
- Changes should be reflected into the [CHANGELOG.md](CHANGELOG.md)

The maintainers
- Bump the [VERSION](VERSION) file if a release is needed

### Building

To create a working binary for your operating system use `make`

This creates a binary in the `./build` folder. Look at the [Build options section](#Build%20options) for more build options.

#### Build options

target  | description
--------|------------
all     | execute `test` and `build` target
build   | use `go build` to create binary for current GOARCH and GOOS in `./build`
test    | use `go test` to execute the unit test and create a coverage report `./build/test-coverage.out`
clean   | clean the build directory
compile | build the binaries for FreeBDS, Linux, MacOS, and Windows in `./build`
dist    | execute `clean` and `compile` targets, and create tar.gz files in `./dist`

### TODO

- add tests

## Changelog

All notable changes for the Galera Health Check can be found in [CHANGELOG.md](CHANGELOG.md).
