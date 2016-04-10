# h2a

**h2a** is a debugging reverse proxy for HTTP/2 developers.  
This can be a great tool to dump h2 frames between client and server.

## Install

Go to the [releases page](https://github.com/summerwind/h2a/releases), find the version you want, and download the zip file.

## Build

1. Make sure you have Go 1.5 and set GOPATH appropriately
2. Run `go get github.com/summerwind/h2a`

It is also possible to build specific version.

1. Make sure you have Go 1.5 and set GOPATH appropriately
2. Run `go get gopkg.in/summerwind/h2a.v1`

## Usage

```
Usage: h2a [OPTIONS]

Options:
  -p:        Port (Default: 443)
  -i:        IP Address (Default: 127.0.0.1)
  -d:        Use HTTP/2 direct mode
  -P:        Origin port
  -H:        Origin host
  -D:        Use HTTP/2 direct mode to connect origin
  -c:        Certificate file
  -k:        Certificate key file
  -o:        Output log format (default or json, Default: default)
  --version: Display version information and exit.
  --help:    Display this help and exit.
```

## Screenshot

This screenshot shows the h2 frames between H2O and Safari 9.

![Screenshot](https://cloud.githubusercontent.com/assets/230145/11783063/669ef676-a2b8-11e5-8c96-45cce86493be.png)
