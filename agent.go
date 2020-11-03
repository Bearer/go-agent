package agent

//go:generate sh generate_sha.sh

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bearer/go-agent/config"
	"github.com/bearer/go-agent/events"
	"github.com/bearer/go-agent/interception"
	"github.com/bearer/go-agent/proxy"
)

const (
	// ExampleWellFormedInvalidKey is a well-formed key known to be invalid. It may
	// be used for integration test scenarios.
	ExampleWellFormedInvalidKey = `app_12345678901234567890123456789012345678901234567890`

	// SecretKeyName is the environment variable used to hold the Bearer secret key,
	// specific to each client. Fetching the secret key from the environment is a
	// best practice in 12-factor application development.
	SecretKeyName = `BEARER_SECRET_KEY`

	// Version is the semantic agent version.
	Version = `1.0.2`
)

type transportMap map[http.RoundTripper]http.RoundTripper

// Agent is the type of the Bearer entry point for your programs.
type Agent struct {
	m             sync.Mutex
	dispatcher    events.Dispatcher
	SecretKey     string
	config        *Config
	baseTransport http.RoundTripper
	transports    transportMap
	error         error
	sender        *proxy.Sender
}

// New constructs a new Agent and returns it.
//
// In most usage scenarios, you will only use a single Agent in a given application,
// and pass a config.WithLogger(some *io.Writer) config.Option.
func New(secretKey string, opts ...Option) *Agent {
	a := &Agent{
		baseTransport: unwrapTransport(http.DefaultClient.Transport),
		dispatcher:    events.NewDispatcher(),
		SecretKey:     secretKey,
		transports:    make(transportMap),
	}

	if !config.IsSecretKeyWellFormed(secretKey) {
		a.setError(errors.New("secret key is not well-formed"))
		return a
	}

	c, err := NewConfig(secretKey, a.baseTransport, Version, opts...)
	if err != nil {
		a.setError(fmt.Errorf("configuring new agent: %w", err))
		return a
	}

	a.config = c
	if c.IsDisabled() {
		a.setError(errors.New(`agent disabled`))
		return a
	}

	a.sender = proxy.NewSender(c.ReportOutstanding, c.ReportEndpoint, Version,
		c.SecretKey(), c.Environment(),
		a.DefaultTransport(), a.Logger())
	go a.sender.Start()

	dcrp := interception.DCRProvider{DCRs: a.config.DataCollectionRules()}
	a.dispatcher.AddProviders(interception.TopicConnect, events.ListenerProviderFunc(a.Provider), dcrp)
	a.dispatcher.AddProviders(interception.TopicRequest, dcrp)
	a.dispatcher.AddProviders(interception.TopicResponse, dcrp)
	a.dispatcher.AddProviders(interception.TopicBodies, interception.BodyParsingProvider{}, dcrp)
	a.dispatcher.AddProviders(interception.TopicReport,
		dcrp,
		interception.SanitizationProvider{
			SensitiveKeys:    a.config.SensitiveKeys(),
			SensitiveRegexps: a.config.SensitiveRegexps(),
		},
		interception.ProxyProvider{Sender: a.sender},
	)

	http.DefaultTransport = a.Decorate(http.DefaultTransport)
	a.DecorateClientTransports(http.DefaultClient)

	return a
}

// DefaultTransport returns the original implementation of the http.DefaultTransport,
// even if it was overridden by the Agent in the meantime.
func (a *Agent) DefaultTransport() http.RoundTripper {
	return a.baseTransport
}

// Decorate wraps a http.RoundTripper with Bearer instrumentation.
func (a *Agent) Decorate(rt http.RoundTripper) http.RoundTripper {
	if a.error != nil {
		return rt
	}

	if rt == nil {
		rt = http.DefaultTransport
	}

	if a.transports == nil {
		a.transports = make(transportMap)
	}

	a.m.Lock()
	defer a.m.Unlock()

	existing, ok := a.transports[rt]
	if ok {
		return existing
	}

	var wrapped = &interception.RoundTripper{
		Dispatcher: a.dispatcher,
		Underlying: rt,
	}

	a.transports[rt] = wrapped
	a.transports[wrapped] = wrapped
	return wrapped
}

// DecorateClientTransports wraps the http.RoundTripper transports in all passed
// clients with Bearer instrumentation.
func (a *Agent) DecorateClientTransports(clients ...*http.Client) {
	if a.error != nil {
		return
	}
	for _, client := range clients {
		client.Transport = a.Decorate(client.Transport)
	}
}

// Error returns any error that has cause the agent to shutdown. If there has
// been no error then it returns nil
func (a *Agent) Error() error {
	return a.error
}

func (a *Agent) setError(err error) {
	a.error = err
	log.Println(err)
}

// Close shuts down the agent
func (a *Agent) Close() error {
	if a.config.IsDisabled() {
		return nil
	}

	a.LogTrace("Bearer agent stopping", nil)

	count := uint(0)
	if a.sender != nil {
		a.sender.Stop()
		count = a.sender.Counter
	}

	a.LogTrace(fmt.Sprintf(`End of Bearer agent operation with %d API calls logged`, count), nil)
	return nil
}

// Provider provides the default agent listeners:
//   - TopicConnect: RFCListener, validating URL under RFC grammars.
//   - TopicRequest, TopicResponse, TopicBodies: no.
func (*Agent) Provider(e events.Event) []events.Listener {
	var l []events.Listener
	switch topic := e.Topic(); topic {
	case interception.TopicConnect:
		l = []events.Listener{
			interception.RFCListener,
		}
	}

	return l
}

func unwrapTransport(rt http.RoundTripper) http.RoundTripper {
	for {
		if base, ok := rt.(*interception.RoundTripper); ok {
			rt = base.Underlying
			continue
		}
		break
	}
	// If the underlying transport, some other package may modify it, so we cannot
	// rely on it being correct afterwards, so provide a default Transport matching
	// the standard values of the http.DefaultTransport.
	if rt == nil {
		rt = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	return rt
}
