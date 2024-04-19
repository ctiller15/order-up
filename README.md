# order-up

Code candidates will extend as part of their technical interview process. The
order-up service handles all order-specific calls including creating orders,
checking the status on orders, etc. This service is part of a larger microservice
backend for a online marketplace.

## Getting started

You also will need to [install Go](https://go.dev/doc/install). Then clone this
this repository and run `go mod tidy` within this repository to download all
necessary dependencies locally.

## Project Structure

### Top-level

The top-level only contains a single `main.go` file which holds the `main`
function. If you ran `go build ./.` that would produce a `order-up` binary that
would start by executing the `main` function in `main.go`.

### api package

The `api` package handles incoming HTTP requests with a REST paradigm and calls
various functions based on the path. This package uses the `storage` package to
perform the necessary functionality for each API call. The tests use a mocked
storage instance.

### storage package

The `storage` package contains the database calls necessary for persisting and
retrieving orders. The methods are missing the bodies of the functions since
you're expected to fill them in with whatever database and implementation you
think satisfies the tests and documented functionality.

### mocks package

The `mocks` package just contains a helper function for mocking an external
service by accepting an http.Handler and returning a *http.Client as well as
generated code for mocking a `*storage.Instance`. This simply makes the tests
easier in the `api` package.

## Relevant Go commands

* [`go mod tidy`](https://go.dev/ref/mod#go-mod-tidy) downloads all dependencies
and update `go.mod` file with any new dependencies
* [`go test -v -race ./...`](https://pkg.go.dev/cmd/go#hdr-Test_packages) tests all
files and subdirectories. You can instead do `go test -v ./storage/...` to only
test the storage package. Any public function with the format `TestX(*testing.T)`
will automatically be called by `go test`. Typically these functions are placed
in `X_test.go` files. You can pass a regex to `-run` like `-run ^TestInsertOrder$`
in order to just run tests matching the regex.
* [`go fmt ./...`](https://pkg.go.dev/cmd/go#hdr-Gofmt__reformat__package_sources)
reformats the go files according to the gofmt spec
* [`go vet ./...`](https://pkg.go.dev/cmd/go#hdr-Report_likely_mistakes_in_packages)
prints out most-likely errors or mistakes in Go code
* [`go get $package`](https://pkg.go.dev/cmd/go#hdr-Add_dependencies_to_current_module_and_install_them)
adds a new dependency to the current project

## Databases

The easiest way to run databases locally for testing is using
[Docker](https://docs.docker.com/get-docker/).
You can use any database you're familiar with these are just some examples.

Alternatively you can sign up for an online free tier of a hosted version of
these databases (like MongoDB Atlas, or CockroachDB Cloud) if that's easier.

### MongoDB

```bash
docker run --rm -it -p 27017:27017 mongo
```

### PostgreSQL

```bash
docker run --rm -it -p 5432:5432 -e POSTGRES_PASSWORD=password postgres
```

### Redis

```bash
docker run --rm -it -p 6379:6379 redis
```

### Relevant Database Packages

You can use any database or driver you're familiar with these are just some
examples.

* [mongo](https://pkg.go.dev/go.mongodb.org/mongo-driver/mongo) and the companion
[bson](https://pkg.go.dev/go.mongodb.org/mongo-driver/bson). See [this tutorial](https://www.mongodb.com/blog/post/mongodb-go-driver-tutorial).
* [database/sql](https://pkg.go.dev/database/sql) but this must be combined with
a driver package like [pq](github.com/lib/pq). See [this tutorial](https://golangdocs.com/golang-postgresql-example)
* [radix](https://pkg.go.dev/github.com/mediocregopher/radix/v4) Redis driver

Remember when you're adding new packages to run `go mod tidy` to ensure the
go.mod and go.sum files are updated.
