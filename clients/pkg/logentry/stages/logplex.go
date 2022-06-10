package stages

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/heroku/x/logplex/encoding"
	"github.com/pkg/errors"
	"strings"
)

// These var names are exposed a vars to be reused in label stages, which require a pointer type.
var (
	LogplexTimestampField   = "__logplex_ts"
	LogplexHostnameField    = "__logplex_host"
	LogplexApplicationField = "__logplex_app"
	LogplexProcessIDField   = "__logplex_proc"
	LogplexLogIDField       = "__logplex_log_id"
	LogplexMessageField     = "__logplex_msg"
)

const (
	ErrMalformedLogplex = "malformed logplex entry"
)

type logplexStage struct {
	logger log.Logger
}

func newLogplexStage(logger log.Logger) (Stage, error) {
	return &logplexStage{
		logger: log.With(logger, "component", "stage", "type", "logplex"),
	}, nil
}

func (l *logplexStage) Name() string {
	return StageTypeLogplex
}

func (l *logplexStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			err := l.processEntry(e.Extracted, e.Line)
			if err != nil {
				// dropping errored stuff
				continue
			}
			out <- e
		}
	}()
	return out
}

func (l *logplexStage) processEntry(extracted map[string]interface{}, line string) error {
	scanner := encoding.NewDrainScanner(strings.NewReader(line))
	for scanner.Scan() {
		message := scanner.Message()
		extracted[LogplexTimestampField] = message.Timestamp
		extracted[LogplexHostnameField] = message.Hostname
		extracted[LogplexApplicationField] = message.Application
		extracted[LogplexProcessIDField] = message.Process
		extracted[LogplexLogIDField] = message.ID
		extracted[LogplexMessageField] = message.Message
	}
	err := scanner.Err()
	if err != nil {
		if Debug {
			level.Debug(l.logger).Log("msg", "failed to unmarshal log line", "err", err)
		}
		return errors.New(ErrMalformedLogplex)
	}
	return nil
}
