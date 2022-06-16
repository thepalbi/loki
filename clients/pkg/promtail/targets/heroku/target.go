package heroku

import (
	"flag"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
	herokuEncoding "github.com/heroku/x/logplex/encoding"
	"github.com/imdario/mergo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/server"
	"net/http"
	"time"
)

var (
	LogplexTimestampField   = "__logplex_ts"
	LogplexHostnameField    = "__logplex_host"
	LogplexApplicationField = "__logplex_app"
	LogplexProcessIDField   = "__logplex_proc"
	LogplexLogIDField       = "__logplex_log_id"
	LogplexMessageField     = "__logplex_msg"
)

type HerokuTarget struct {
	logger  log.Logger
	handler api.EntryHandler
	config  *scrapeconfig.HerokuTargetConfig
	jobName string
	server  *server.Server
}

func NewHerokuTarget(logger log.Logger,
	handler api.EntryHandler,
	jobName string,
	config *scrapeconfig.HerokuTargetConfig,
) (*HerokuTarget, error) {

	pt := &HerokuTarget{
		logger:  logger,
		handler: handler,
		jobName: jobName,
		config:  config,
	}

	// Bit of a chicken and egg problem trying to register the defaults and apply overrides from the loaded config.
	// First create an empty config and set defaults.
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	// Then apply any config values loaded as overrides to the defaults.
	if err := mergo.Merge(&defaults, config.Server, mergo.WithOverride); err != nil {
		level.Error(logger).Log("msg", "failed to parse configs and override defaults when configuring push server", "err", err)
	}
	// The merge won't overwrite with a zero value but in the case of ports 0 value
	// indicates the desire for a random port so reset these to zero if the incoming config val is 0
	if config.Server.HTTPListenPort == 0 {
		defaults.HTTPListenPort = 0
	}
	if config.Server.GRPCListenPort == 0 {
		defaults.GRPCListenPort = 0
	}
	// Set the config to the new combined config.
	config.Server = defaults

	err := pt.run()
	if err != nil {
		return nil, err
	}

	return pt, nil
}

func (h *HerokuTarget) run() error {
	level.Info(h.logger).Log("msg", "starting push server", "job", h.jobName)
	// To prevent metric collisions because all metrics are going to be registered in the global Prometheus registry.
	h.config.Server.MetricsNamespace = "promtail_" + h.jobName

	// We don't want the /debug and /metrics endpoints running
	h.config.Server.RegisterInstrumentation = false

	// The logger registers a metric which will cause a duplicate registry panic unless we provide an empty registry
	// The metric created is for counting log lines and isn't likely to be missed.
	util_log.InitLogger(&h.config.Server, prometheus.NewRegistry())

	srv, err := server.New(h.config.Server)
	if err != nil {
		return err
	}

	h.server = srv
	h.server.HTTP.Path("/heroku/api/v1/drain").Methods("POST").Handler(http.HandlerFunc(h.drain))

	go func() {
		err := srv.Run()
		if err != nil {
			level.Error(h.logger).Log("msg", "Loki push server shutdown with error", "err", err)
		}
	}()

	return nil
}

func (h *HerokuTarget) drain(w http.ResponseWriter, r *http.Request) {
	entries := h.handler.Chan()
	defer r.Body.Close()
	herokuScanner := herokuEncoding.NewDrainScanner(r.Body)
	for herokuScanner.Scan() {
		message := herokuScanner.Message()
		ls := model.LabelSet{}
		ls[model.LabelName(LogplexTimestampField)] = model.LabelValue(message.Timestamp.Format(time.RFC3339Nano))
		ls[model.LabelName(LogplexHostnameField)] = model.LabelValue(message.Hostname)
		ls[model.LabelName(LogplexApplicationField)] = model.LabelValue(message.Application)
		ls[model.LabelName(LogplexProcessIDField)] = model.LabelValue(message.Process)
		ls[model.LabelName(LogplexLogIDField)] = model.LabelValue(message.ID)
		entries <- api.Entry{
			Labels: h.Labels().Merge(ls),
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      message.Message,
			},
		}
	}
	err := herokuScanner.Err()
	if err != nil {
		level.Warn(h.logger).Log("msg", "failed to read incoming push request", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HerokuTarget) Type() target.TargetType {
	return target.HerokuDrainTargetType
}

func (h *HerokuTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

func (h *HerokuTarget) Labels() model.LabelSet {
	return h.config.Labels
}

func (h *HerokuTarget) Ready() bool {
	return true
}

func (h *HerokuTarget) Details() interface{} {
	return map[string]string{}
}

func (h *HerokuTarget) Stop() error {
	level.Info(h.logger).Log("msg", "stopping push server", "job", h.jobName)
	h.server.Shutdown()
	h.handler.Stop()
	return nil
}
