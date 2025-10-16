
# jtimon

Junos Telemetry Interface client

## Setup

```sh
$ git clone https://github.com/Juniper/jtimon.git
$ cd jtimon
$ make linux or make darwin
$ ./jtimon-linux-amd64 --help or jtimon-darwin-amd64 --help
```

Please note that if you use make to build source, it will produce binary with GOOS and GOARCH names e.g. jtimon-darwin-amd64, jtimon-linux-amd64 etc. Building the source using make is recommended as it will insert git-revision, build-time info in the binary.

To understand what targets are available in make, run the make help command as follows:

```sh
$ make help
```

### Note

If you are cloning the source and building it, please make sure you have environment variable GOPATH is set correctly.
https://golang.org/doc/code.html#GOPATH

## Docker container

Alternatively to building jtimon native, one can build a jtimon Docker container and run it dockerized while passing the local directory to the container to access the json file.

To build the container:

```sh
make docker
```

Check the resulting image:

```sh
$ docker images jtimon
REPOSITORY          TAG                 IMAGE ID            CREATED             SIZE
jtimon              latest              f1a794609339        7 minutes ago       24.5MB
```

Run it:

```sh
docker run -ti --rm -v ${PWD}:/u:ro jtimon --help
```

Or simply by calling ./jtimon, which is a symlink to launch-docker-container.sh, capable of launching the container by name with the current directory mounted into /u:

```sh
$ ./jtimon
Enter config file name: bla.json
2018/03/02 13:53:44 File error: open bla.json: no such file or directory
```

## CLI Options

```
$ ./jtimon-linux-amd64 --help
Usage of ./jtimon-linux-amd64:
      --compression string         Enable HTTP/2 compression (gzip)
      --config strings             Config file name(s)
      --config-file-list string    List of Config files
      --consume-test-data          Consume test data
      --explore-config             Explore full config of JTIMON and exit
      --generate-test-data         Generate test data
      --json                       Convert telemetry packet into JSON
      --log-mux-stdout             All logs to stdout
      --max-run int                Max run time in seconds
      --no-per-packet-goroutines   Spawn per packet go routines
      --pprof                      Profile JTIMON
      --pprof-port int32           Profile port (default 6060)
      --prefix-check               Report missing __prefix__ in telemetry packet
      --print                      Print Telemetry data
      --prometheus                 Stats for prometheus monitoring system
      --prometheus-host string     IP to bind Prometheus service to (default "127.0.0.1")
      --prometheus-port int32      Prometheus port (default 8090)
      --stats-handler              Use GRPC statshandler
      --version                    Print version and build-time of the binary and exit
pflag: help requested
```

## Config

To explore what can go in config, please use --explore-config option.

Except connection details like host, port, etc no other part of the config is mandatory e.g. do not use influx in your config if you dont want to insert data into it.

```
$ ./jtimon-linux-amd64 --explore-config                                                                                                                                   [8/1981]
2021/10/02 17:15:22 Version: v2.3.0-7bfd8fdf2fcae1d55079e3d9eceb761be0842eae-master BuildTime 2021-10-02T19:51:16-0400
2021/10/02 17:15:22
{
    "port": 0,
    "host": "",
    "user": "",
    "password": "",
    "cid": "",
    "meta": false,
    "eos": false,
    "grpc": {
        "ws": 0
    },
    "tls": {
        "clientcrt": "",
        "clientkey": "",
        "ca": "",
        "servername": ""
    },
    "influx": {
        "server": "",
        "port": 0,
        "dbname": "",
        "user": "",
        "password": "",
        "recreate": false,
        "measurement": "",
        "batchsize": 0,
        "batchfrequency": 0,
        "http-timeout": 0,
        "retention-policy": "",
        "accumulator-frequency": 0,
        "write-per-measurement": false
    },
    "kafka": null,
    "paths": [
        {
            "path": "",
            "freq": 0,
            "mode": ""
        }
    ],
    "log": {
        "file": "",
        "periodic-stats": 0,
        "verbose": false
    },
    "vendor": {
        "name": "",
        "remove-namespace": false,
        "schema": null,
        "gnmi": null
    },
    "alias": "",
    "password-decoder": "",
    "enable-uint": false
}    
```

I am explaining some config options which are not self-explanatory.

<pre>
meta : send username and password over gRPC meta instead of invoking LoginCheck() RPC for authentication. 
Please use SSL/TLS for security. For more details on how to use SSL/TLS, please refer wiki
https://github.com/Juniper/jtimon/wiki/SSL
</pre>

<pre>
cid : client id. Junos expects unique client ids if multiple clients are subscribing to telemetry streams.
</pre>

<pre>
eos : end of sync. Tell Junos to send end of sync for on-change subscriptions.
</pre>

<pre>
grpc/ws : window size of grpc for slower clients
</pre>


## IPv6 Support

jtimon supports both IPv4 and IPv6 addresses for connecting to network devices.

### IPv4 Configuration
```json
{
    "host": "192.168.1.1",
    "port": 32767,
    "user": "username",
    "password": "password",
    "paths": [
        {
            "path": "/interfaces",
            "freq": 10000
        }
    ]
}
```

### IPv6 Configuration
When using IPv6 addresses, wrap the address in square brackets `[]`:

```json
{
    "host": "[2001:db8::1]",
    "port": 32767,
    "user": "username", 
    "password": "password",
    "paths": [
        {
            "path": "/interfaces",
            "freq": 10000
        }
    ]
}
```

**Note**: The square brackets are required for IPv6 addresses to properly distinguish the address from the port number, following RFC 3986 standards.

### Examples of Valid IPv6 Formats
- `"[::1]"` - IPv6 loopback
- `"[2001:db8::1]"` - Full IPv6 address
- `"[fe80::1%eth0]"` - Link-local with zone identifier
- `"[::ffff:192.0.2.1]"` - IPv4-mapped IPv6 address

### Command Line Usage
When using command line options for dial-out or server mode:
```bash
# IPv4
./jtimon --host 127.0.0.1 --port 32767

# IPv6  
./jtimon --host ::1 --port 32767
```

## Kafka Publish

To publish gRPC/Openconfig JTI data to Kafka, use the following json config.

```
$ cat kafka-test-1.json
{
    "host": "2.2.2.2",
    "port": 32767,
    "user": "username",
    "password": "password",
    "cid": "cid123",
    "kafka": {
        "brokers": ["1.1.1.1:9094"],
        "topic": "test",
        "client-id": "testjtimonmx86"
    },

    "paths": [
        {
            "path": "/interfaces",
            "freq": 10000
        }
    ]
}
```

Below are all possible Kafka config options.

```
type KafkaConfig struct {
	Version            string   `json:"version"`
	Brokers            []string `json:"brokers"`
	ClientID           string   `json:"client-id"`
	Topic              string   `json:"topic"`
	CompressionCodec   int      `json:"compression-codec"`
	RequiredAcks       int      `json:"required-acks"`
	MaxRetry           int      `json:"max-retry"`
	MaxMessageBytes    int      `json:"max-message-bytes"`
	SASLUser           string   `json:"sasl-username"`
	SASLPass           string   `json:"sasl-password"`
	TLSCA              string   `json:"tls-ca"`
	TLSCert            string   `json:"tls-cert"`
	TLSKey             string   `json:"tls-key"`
	InsecureSkipVerify bool     `json:"insecure-skip-verify"`
}
```
