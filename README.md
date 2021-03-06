# jtimon

Junos Telemetry Interface client

## Setup

```sh
go get github.com/Juniper/jtimon
$GOPATH/bin/jtimon --help
```

OR

```sh
git clone https://github.com/Juniper/jtimon.git
cd jtimon
go build or make
./jtimon --help
```

Please note that if you use make to build source, it will produce binary with GOOS and GOARCH names e.g. jtimon-darwin-amd64, jtimon-linux-amd64 etc. Building the source using make is recommended as it will insert git-revision, build-time info in the binary.

To understand what targets are available in make, run the make help command as follows:

```sh
make help
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
jtimon              latest              3b7622e1464f        6 minutes ago       174MB
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

```sh
$ ./jtimon --help
Usage of ./jtimon:
      --api                     Receive HTTP commands when running
      --compression string      Enable HTTP/2 compression (gzip, deflate)
      --config strings          Config file name(s)
      --explore-config          Explore full config of JTIMON and exit
      --gnmi                    Use gnmi proto
      --gnmi-encoding string    gnmi encoding (proto | json | bytes | ascii | ietf-json (default "proto")
      --gnmi-mode string        Mode of gnmi (stream | once | poll (default "stream")
      --grpc-headers            Add grpc headers in DB
      --gtrace                  Collect GRPC traces
      --json                    Convert telemetry packet into JSON
      --latency-profile         Profile latencies. Place them in TSDB
      --log-mux-stdout          All logs to stdout
      --max-run int             Max run time in seconds
      --pprof                   Profile JTIMON
      --pprof-port int32        Profile port (default 6060)
      --prefix-check            Report missing __prefix__ in telemetry packet
      --print                   Print Telemetry data
      --prometheus              Stats for prometheus monitoring system
      --prometheus-port int32   Prometheus port (default 8090)
      --stats-handler           Use GRPC statshandler
      --version                 Print version and build-time of the binary and exit
pflag: help requested
```

## Config

To explore what can go in config, please use --explore-config option.

Except connection details like host, port, etc no other part of the config is mandatory e.g. do not use influx in your config if you dont want to insert data into it.

```sh
$ ./jtimon-darwin-amd64 --explore-config
Version: 0ba993049ca2ac9690b0440df88ae4f5c3d26d37-master BuildTime 2018-05-23T23:47:27-0700

{
    "host": "",
    "port": 0,
    "user": "",
    "password": "",
    "meta": false,
    "eos": false,
    "cid": "",
    "api": {
        "port": 0
    },
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
        "diet": false,
        "batchsize": 0,
        "batchfrequency": 0,
        "retention-policy": ""
    },
    "paths": [
        {
            "path": "",
            "freq": 0,
            "mode": ""
        }
    ],
    "log": {
        "file": "",
        "verbose": false,
        "periodic-stats": 0,
        "drop-check": false,
        "latency-check": false,
        "csv-stats": false,
        "FileHandle": null,
        "Logger": null
    }
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
