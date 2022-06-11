package stages

import (
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

var testLogplexYaml = `
pipeline_stages:
- logplex:
`

func TestPipeline_Logplex(t *testing.T) {
	t.Parallel()
	parsedDate, err := time.Parse(time.RFC3339, "2012-11-30T06:45:29+00:00")
	require.NoError(t, err, "expected test heroku date to be parsed correctly")

	tests := map[string]struct {
		entry           string
		expectedExtract map[string]interface{}
	}{
		"empty line causes no data to be extracted": {
			entry:           "",
			expectedExtract: map[string]interface{}{},
		},
		"some test": {
			entry: `83 <40>1 2012-11-30T06:45:29+00:00 host app web.3 - State changed from starting to up
119 <40>1 2012-11-30T06:45:26+00:00 host app web.3 - Starting process with command "bundle exec rackup config.ru -p 24405"
`,
			expectedExtract: map[string]interface{}{},
		},
		"heroku example is parsed correctly": {
			entry: "83 <40>1 2012-11-30T06:45:29+00:00 host app web.3 - State changed from starting to up",
			expectedExtract: map[string]interface{}{
				LogplexMessageField:     "State changed from starting to up ",
				LogplexProcessIDField:   "web.3",
				LogplexLogIDField:       "-",
				LogplexApplicationField: "app",
				LogplexHostnameField:    "host",
				LogplexTimestampField:   parsedDate,
			},
		},
		"test app example is parsed correctly": {
			entry:           "69 <134>1 2022-06-10T20:50:53.690097+00:00 host heroku web.1 - Unidling",
			expectedExtract: map[string]interface{}{},
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			pl, err := NewPipeline(log.NewLogfmtLogger(os.Stderr), loadConfig(testLogplexYaml), nil, prometheus.DefaultRegisterer)
			assert.NoError(t, err, "Expected pipeline creation to not result in error")
			out := processEntries(pl, newEntry(nil, nil, testData.entry, time.Now()))[0]
			assert.Equal(t, testData.expectedExtract, out.Extracted)
		})
	}
}
