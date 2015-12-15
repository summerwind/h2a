# h2a

**h2a** is a debugging reverse proxy for HTTP/2 developers.  
h2a can be a great tool to dump h2 frames between client and server.

## Install

Go to the [releases page](https://github.com/summerwind/h2a/releases), find the version you want, and download the zip file.

## Build

1. Make sure you have go 1.5 and set GOPATH appropriately
2. Run go get github.com/summerwind/h2a

It is also possible to build specific version.

1. Make sure you have go 1.5 and set GOPATH appropriately
2. Run go get gopkg.in/summerwind/h2a.v0

## Usage

```
Usage: h2a [OPTIONS]

Options:
  -p:        Port. (Default: 443)
  -i:        IP Address. (Default: 127.0.0.1)
  -P:        Origin port.
  -H:        Origin host.
  -c:        Certificate file.
  -k:        Certificate key file.
  --version: Display version information and exit.
  --help:    Display this help and exit.
```

## Screenshot

![Screenshot](https://cloud.githubusercontent.com/assets/230145/11783063/669ef676-a2b8-11e5-8c96-45cce86493be.png)