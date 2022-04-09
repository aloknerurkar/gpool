# gpool [![Go](https://github.com/aloknerurkar/gpool/workflows/Go/badge.svg)](https://github.com/aloknerurkar/gpool/actions) [![Go Reference](https://pkg.go.dev/badge/github.com/aloknerurkar/gpool.svg)](https://pkg.go.dev/github.com/aloknerurkar/gpool) [![Coverage Status](https://coveralls.io/repos/github/aloknerurkar/gpool/badge.svg?branch=main)](https://coveralls.io/github/aloknerurkar/gpool?branch=main)
Generic pool implementation

gpool implements a generic pool of objects. With generics this removes the ugly
type checking required for xsync.Pool and also adds additional functionality like
setting upper limits on active/idle objects and also allowing users to configure
constructors, destructors and on get hooks to add extra checking while accessing
these objects

## Install
`gpool` works like a regular Go module:

```
> go get github.com/aloknerurkar/gpool
```

## Usage
```go
import "github.com/aloknerurkar/gpool"
```
