// Package proxy handles the transmission of ReportLog collected data to the
// Bearer platform.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/bearer/go-agent/events"
	"github.com/bearer/go-agent/filters"
)

const (
	// AckBacklog is the capacity of the log write acknowledgments channel.
	AckBacklog = 1000
	// FanInBacklog is the capacity of the fan-in log write channel
	FanInBacklog = 100

	// Success is the ReportLog Type for successful API calls.
	Success = `REQUEST_SUCCESS`
	// Error is the ReportLog Type for failed API calls.
	Error = `REQUEST_ERROR`
	// Loss is the ReportLog Type for synthetic reports warning of reports loss.
	Loss = `REPORT_LOSS`

	// AcceptHeader is the canonical Accept header name.
	AcceptHeader = `Accept`

	// ContentTypeHeader is the canonical content type header name.
	ContentTypeHeader = `Content-Type`

	// ContentTypeJSON is the canonical content type header value for JSON.
	ContentTypeJSON = `application/json`

	// FullContentTypeJSON is the content type for JSON when emitting it.
	FullContentTypeJSON = `application/json; charset=utf-8`
)

// MustParseURL builds a URL instance from a known-good URL string, panicking it
// the URL string is not well-formed.
func MustParseURL(rawURL string) *url.URL {
	maybeURL, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return maybeURL
}

// Sender is the control structure for the background log writing loop.
type Sender struct {
	// Finish is used to transmit the app termination request to the background
	// sending loop.
	Finish chan bool

	// Done is used by the background sending loop to confirm it is done, allowing
	// the agent.Close function to await clean Sender flush if it wishes.
	Done chan bool

	// FanIn receives the ReportLog elements to send from all the goroutines
	// created on API calls termination, serializing them to the background sending loop.
	FanIn chan ReportLog

	// Acks receives the acknowledgments from the HTTP sending the marshaled
	// ReportLog elements to the Bearer platform.
	//
	// In this version, each element has value 1. When sending gets to be batched
	// in a later version, this value will represent the number of acknowledged
	// LogReport elements actually transmitted.
	Acks chan uint

	// InFlight is the number of ReportLog elements awaiting delivery to the
	// Bearer platform.
	InFlight uint

	// Lost is the number of lost and never sent ReportLog elements. It is reset
	// to 0 when transmission resumes.
	Lost uint

	// Configuration fields below.

	// InflightLimit is the maximum value of Inflight before bandwidth reduction
	// is triggered. When InFlight exceeds this value, extra ReportLog elements
	// are dropped, only counting the number of lost elements, to avoid saturation
	// of the client process and network.
	InFlightLimit uint

	// LogEndpoint is the URL of the Bearer host receiving the logs.
	LogEndpoint string

	// EnvironmentType is the runtime environment type, e.g. staging or production.
	EnvironmentType string

	// SecretKey is the account secret key.
	SecretKey string

	// Version is the agent version.
	Version string

	http.Client
	events.Dispatcher
	*zerolog.Logger
}

// Stop notifies the background sending loop that the application is shutting
// down. Any ReportLog elements send after that call will be lost and unreported.
func (s *Sender) Stop() {
	s.Done <- true
	s.FanIn = nil
}

// NewSender builds a ready-to-user
func NewSender(
	limit uint, endPoint string, version string, secretKey string, environmentType string,
	transport http.RoundTripper, dispatcher events.Dispatcher, logger *zerolog.Logger,
) *Sender {
	s := Sender{
		Finish:          make(chan bool, 1),
		Done:            make(chan bool, 1),
		FanIn:           make(chan ReportLog, FanInBacklog),
		Acks:            make(chan uint, AckBacklog),
		InFlightLimit:   limit,
		LogEndpoint:     MustParseURL(endPoint).String(),
		EnvironmentType: environmentType,
		SecretKey:       secretKey,
		Version:         version,
		Client:          http.Client{Transport: transport},
		Dispatcher:      dispatcher,
		Logger:          logger,
	}
	return &s
}

// Send sends a ReportLog element to the FanIn channel for transmission.
// It should not be called after Stop.
func (s *Sender) Send(log ReportLog) {
	if s.FanIn == nil {
		s.Warn().Msg(`sending attempted after Stop: ignored`)
	}
	go func() {
		s.FanIn <- log
	}()
}

// Start configures and starts the background sending loop.
func (s *Sender) Start() {
	defer func() {
		// Will not block, because channel has len 1
		s.Done <- true
	}()

	// Normal operation.
Normal:
	for {
		s.Logger.Debug().Msg("Proxy loop")
		select {
		// Finish received: switch to Finishing mode.
		case <-s.Finish:
			s.Logger.Debug().Msg("Sender switching to Finishing mode.")
			break Normal

		// ReportLog to write.
		case rl := <-s.FanIn:
			s.Logger.Debug().Msg("Sender received log.")
			if s.InFlight >= s.InFlightLimit {
				s.Lost++
				continue
			}
			s.InFlight++
			go s.WriteLog(rl)

		// Acknowledgment of ReportLog written.
		case n := <-s.Acks:
			s.Logger.Debug().Msg("Sender received ack.")
			if n == 0 {
				s.Error().Msg("received an acknowledgment for 0 report")
				continue
			}
			if n > s.InFlight {
				// This should never happen, except for bugs.
				s.Error().Msgf(`%d reports acknowledged, but only %d were in flight`, n, s.InFlight)
				n = s.InFlight
			}
			// First window of opportunity to transmit a loss report.
			s.InFlight -= n
			if s.Lost > 0 {
				s.InFlight++
				go s.WriteLog(NewReportLossReport(s.Lost))
				s.Lost = 0
			}
		}
	}

	// Finishing.
	for {
		select {
		// ReportLog to write. Same as normal operation.
		case rl := <-s.FanIn:
			s.Logger.Debug().Msg("Finishing sender received log.")
			if s.InFlight >= s.InFlightLimit {
				s.Lost++
				continue
			}
			s.InFlight++
			go s.WriteLog(rl)

		case n := <-s.Acks:
			s.Logger.Debug().Msg("Finishing sender received ack.")
			if n == 0 {
				s.Error().Msg("received an acknowledgment in finishing phase but for 0 report")
				continue
			}
			if n > s.InFlight {
				// This should never happen, except for bugs.
				s.Error().Msgf(`%d reports acknowledged in finishing phase, but only %d were in flight`, n, s.InFlight)
				n = s.InFlight
			}
			s.InFlight -= n
			if s.Lost > 0 {
				s.InFlight++
				go s.WriteLog(NewReportLossReport(s.Lost))
				s.Lost = 0
			}
			if len(s.FanIn) == 0 {
				return
			}
		}
	}

}

// WriteLog attempts to transmit a ReportLog to the Bearer platform, and acknowleges
// it finished its attempt, whether it succeeded or not.
func (s *Sender) WriteLog(rl ReportLog) {
	defer func() {
		// The attempt was made, the request is no longer outstanding even if it failed.
		s.Acks <- 1
	}()

	lr := MakeConfigReport(s.Version, s.EnvironmentType)
	lr.SecretKey = s.SecretKey
	lr.Logs = []ReportLog{rl}

	body, err := json.Marshal(lr)
	if err != nil {
		s := err.Error()
		msg := struct{ Error string }{Error: s}
		body, _ = json.Marshal(msg)
	}

	req, err := http.NewRequest(http.MethodPost, s.LogEndpoint, bytes.NewReader(body))
	if err != nil {
		s.Warn().Err(err).Msg(`error building the log request`)
		return
	}
	req.Header.Add(AcceptHeader, ContentTypeJSON)
	req.Header.Set(ContentTypeHeader, FullContentTypeJSON)
	_, err = s.Client.Do(req)

	if err != nil {
		s.Warn().Err(err).Msg(`transmitting logs to the report server.`)
	} else {
		s.Debug().RawJSON("report", body).Msg("Log uploaded")
	}
}

// NewReportLossReport creates an off-API ReportLog for lost records.
func NewReportLossReport(n uint) ReportLog {
	return ReportLog{
		Type:             Loss,
		Stage:            filters.StageUndefined,
		ErrorCode:        strconv.Itoa(int(n)),
		ErrorFullMessage: fmt.Sprintf("%d report logs were logs", n),
	}
}

// ReportLog is the report summarizing an API call.
type ReportLog struct {
	LogLevel string

	// Common, except for Detected level.

	StartedAt, EndedAt time.Time
	Type               string // REQUEST_END on success, REQUEST_ERROR on connection errors
	filters.Stage
	ActiveDataCollectionRules []string // More compact than sending the complete rule.

	// filters.StageConnect

	Port     uint16
	Protocol string // Scheme: http[s]
	Hostname string

	// filters.StageRequest

	Path           string
	Method         string
	URL            string
	RequestHeaders http.Header

	// filters.StageResponse

	ResponseHeaders http.Header
	StatusCode      int

	// filters.StageBodies
	RequestBody  []byte
	ResponseBody []byte
	// Payload SHAs
	RequestBodyPayloadSHA  []byte
	ResponseBodyPayloadSHA []byte

	// Error
	ErrorCode        string
	ErrorFullMessage string
}
