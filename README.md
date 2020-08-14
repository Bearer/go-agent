# Bearer.sh Go agent

[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/bearer/go-agent)

This module provides a pure Go HTTP (HTTPS, HTTP/2) transport decorator for Go
Web API clients. It relies on the [Bearer.sh](https://www.bearer.sh) platform
to provide metrics observation and anomaly detection.


## Getting started

Register on https://login.bearer.sh/login to obtain an account and its secret key.

Then, with that key in hand, just start your program code with a simple snippet:

```go
package main

import bearer `github.com/bearer/go-agent`

func main() {
    agent := bearer.New(`app_50-digits-long-secret-key-from-bearer.sh`)
    defer agent.Close()

    // From then on, all your http.Get and similar calls are instrumented.
}
```

This will provide instrumentation for any clients using the
`http.DefaultTransport` provided by the Go standard library.


## Fully configurable version

If your needs go beyond the default Go transport, you can create your own instrumented
transport, and use it in your custom-created HTTP clients:

```go
package main

import (
  "net/http"
  "os"
  "time"

  bearer "github.com/bearer/go-agent"
)

func main() {
  // Step 1: initialize Bearer.
  agent := bearer.New(os.Getenv(bearer.SecretKeyName))
  defer agent.Close()

  // Step 2: prepare your custom transport.
  baseTransport := &http.Transport{
    TLSHandshakeTimeout: 5 * time.Second,
  }

  // Step 3: decorate your transport with the Bearer agent.
  transport := agent.Decorate(baseTransport)

  // Step 4: use your client as usual.
  client := http.Client{Transport: transport}
  response, err := client.Get(`https://some.example.com/path`)

  // ...use the API response and error as usual
}
```

For even more advanced use cases, the API also allows creating multiple
configurations and multiple Bearer agents.


## Privacy considerations  / GDPR

Since logging API calls may involve sensitive data, you may want to configure
custom filters to strip [PII](https://gdpr.eu/eu-gdpr-personal-data/) from the
logs, using the `SensitiveKeys` and `SensitiveRegex` options on the agent.


## Deployment

On a live system, you will likely apply two best practices:

- Take the secret key from the environment. We suggest calling the variable
  `BEARER_SECRET_KEY`, for which the `SecretKeyName` constant is available in the
  `config` package.
- For logging
  - either use the default agent logging, which goes to standard error output
    (12-factor suggests standard output), whence messages can be picked up,
  - or apply a frequent deviation from 12-factor by injecting a logger of your
    choice to the configuration in `config/NewConfig()` and, ideally, also
    injecting it to the default Go logger using `log.SetOutput(myLogger)` to
    ensure logs consistency.

Your firewall will need to allow your application to perform outgoing HTTPS/HTTP2
calls to the Bearer platform, at `https://config.bearer.sh` and `https://logs.bearer.sh`.


## Prerequisites

- For your applications:
  - Go 1.13 or later
  - Go modules enabled
- To contribute to the agent
  - Go [`stringer`](https://pkg.go.dev/golang.org/x/tools/cmd?tab=overview) command
  - The `make` command
  - To rebuild the import graph
    - [Godepgraph](https://github.com/kisielk/godepgraph) command
    - [Graphviz](https://graphviz.org/)
  - To check syntax and coding standards:
    - [Golint](https://github.com/golang/lint)
    - [Golangci-lint](https://github.com/golangci/golangci-lint)


## Contributing

### Running the tests

The run the 540+ tests in the package, you can use `go test` if you wish, or run
the preconfigured `go test` command from the `Makefile`:

```
$ make test
```

(this runs the tests with the race detector)


### Run coding style tests

These tests verify that the code base applies best practices: `make lint`

This should just show the command being run, and display no warnings.


### Versioning

We use [SemVer](http://semver.org/) for versioning. For the versions available,
see the [tags on this repository](https://code.osinet.fr/OSInet/bearer-go-agent/releases).

- Versions 2.m.p are the current stable versions.
- Versions 0.m.p and 1.m.p use the original PoC code base and are now obsolete.

When preparing to commit a branch, commit all files, then, run `go generate agent.go`
to generate the `agent_sha.go` file containing the commit SHA, and add a commit
with that information, not squashing it. Users will be able to report that SHA
to support to enable them to be sure of the version of the agent actually in use.


## Credits / Legal

### Authors

- **Frédéric G. MARAND** - *Project development* - [OSInet](https://osinet.fr/go)
- **Manfred TOURON** - *PoC version* - [Manfred.life](https://manfred.life)
- **Bearer.sh** - https://www.bearer.sh


### License

This project is published under the Apache 2.0 License - see the [LICENSE](LICENSE) file for details


### Acknowledgments

- The events package is very much inspired by the
  [PSR-14](https://www.php-fig.org/psr/psr-14/) specification, published under
  the CC-BY-3.0 UNPORTED license for text and MIT License for code.
- The dependency resolution algorithm in `Description.resolveHashes` takes
  inspiration from an [article by Ferry Boender](https://www.electricmonk.nl/docs/dependency_resolving_algorithm/dependency_resolving_algorithm.html)
- The well-formed invalid credit card numbers used for sensitive data validation
  were provided by https://www.freeformatter.com/credit-card-number-generator-validator.html
