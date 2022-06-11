package heroku

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/prometheus/client_golang/prometheus"
)

type HerokuDrainTargetManager struct {
	logger  log.Logger
	targets map[string]*HerokuDrainTarget
}

func NewHerokuDrainTargetManager(
	reg prometheus.Registerer,
	logger log.Logger,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config) (*HerokuDrainTargetManager, error) {

	tm := &HerokuDrainTargetManager{
		logger:  logger,
		targets: make(map[string]*HerokuDrainTarget),
	}

	for _, cfg := range scrapeConfigs {
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "push_pipeline_"+cfg.JobName), cfg.PipelineStages, &cfg.JobName, reg)
		if err != nil {
			return nil, err
		}

		t, err := NewHerokuDrainTarget(logger, pipeline.Wrap(client), cfg.JobName, cfg.HerokuDrainConfig)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
	}

	return tm, nil
}

func (hm *HerokuDrainTargetManager) Ready() bool {
	for _, t := range hm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

func (hm *HerokuDrainTargetManager) Stop() {
	for name, t := range hm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("event", "failed to stop pubsub target", "name", name, "cause", err)
		}
	}
}

func (hm *HerokuDrainTargetManager) ActiveTargets() map[string][]target.Target {
	return hm.AllTargets()
}

func (hm *HerokuDrainTargetManager) AllTargets() map[string][]target.Target {
	res := make(map[string][]target.Target, len(hm.targets))
	for k, v := range hm.targets {
		res[k] = []target.Target{v}
	}
	return res
}
