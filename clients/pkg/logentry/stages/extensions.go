package stages

import (
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	RFC3339Nano = "RFC3339Nano"
	RFC3339     = "RFC3339"
)

// NewDocker creates a Docker json log format specific pipeline stage.
func NewDocker(logger log.Logger, registerer prometheus.Registerer) (Stage, error) {
	stages := PipelineStages{
		PipelineStage{
			StageTypeJSON: JSONConfig{
				Expressions: map[string]string{
					"output":    "log",
					"stream":    "stream",
					"timestamp": "time",
				},
			}},
		PipelineStage{
			StageTypeLabel: LabelsConfig{
				"stream": nil,
			}},
		PipelineStage{
			StageTypeTimestamp: TimestampConfig{
				Source: "timestamp",
				Format: RFC3339Nano,
			}},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"output",
			},
		}}
	return NewPipeline(logger, stages, nil, registerer)
}

// NewCRI creates a CRI format specific pipeline stage
func NewCRI(logger log.Logger, registerer prometheus.Registerer) (Stage, error) {
	stages := PipelineStages{
		PipelineStage{
			StageTypeRegex: RegexConfig{
				Expression: "^(?s)(?P<time>\\S+?) (?P<stream>stdout|stderr) (?P<flags>\\S+?) (?P<content>.*)$",
			},
		},
		PipelineStage{
			StageTypeLabel: LabelsConfig{
				"stream": nil,
			},
		},
		PipelineStage{
			StageTypeTimestamp: TimestampConfig{
				Source: "time",
				Format: RFC3339Nano,
			},
		},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				"content",
			},
		},
	}
	return NewPipeline(logger, stages, nil, registerer)
}

// NewHerokuDrain creates a new Heroku LogPlex drain pipeline stage
func NewHerokuDrain(logger log.Logger, registerer prometheus.Registerer) (Stage, error) {
	stages := PipelineStages{
		PipelineStage{
			StageTypeLogplex: nil,
		},
		PipelineStage{
			StageTypeLabel: LabelsConfig{
				"host":   &LogplexHostnameField,
				"app":    &LogplexApplicationField,
				"proc":   &LogplexProcessIDField,
				"log_id": &LogplexLogIDField,
			},
		},
		PipelineStage{
			StageTypeTimestamp: TimestampConfig{
				Source: LogplexTimestampField,
				Format: RFC3339,
			},
		},
		PipelineStage{
			StageTypeOutput: OutputConfig{
				LogplexMessageField,
			},
		},
	}
	return NewPipeline(logger, stages, nil, registerer)
}
