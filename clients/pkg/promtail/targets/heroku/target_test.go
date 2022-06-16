package heroku

import (
	"flag"
	"fmt"
	"github.com/go-kit/log"
	"github.com/google/uuid"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const localhost = "127.0.0.1"

const testPayload = `270 <158>1 2022-06-13T14:52:23.622778+00:00 host heroku router - at=info method=GET path="/" host=cryptic-cliffs-27764.herokuapp.com request_id=59da6323-2bc4-4143-8677-cc66ccfb115f fwd="181.167.87.140" dyno=web.1 connect=0ms service=3ms status=200 bytes=6979 protocol=https
`
const testLogLine1 = `140 <190>1 2022-06-13T14:52:23.621815+00:00 host app web.1 - [GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`
const testLogLine2 = `156 <190>1 2022-06-13T14:52:23.827271+00:00 host app web.1 - [GIN] 2022/06/13 - 14:52:23 | 200 |      163.92µs |  181.167.87.140 | GET      "/static/main.css"
`

func makeDrainRequest(host string, bodies ...string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/heroku/api/v1/drain", host), strings.NewReader(strings.Join(bodies, "")))
	if err != nil {
		return nil, err
	}

	/*
		Headers received from drain messages

			Content-Type: application/logplex-1
			Logplex-Drain-Token: d.315c56cc-6553-4a07-ac60-af4fd23a9921
			Logplex-Frame-Id: adc10463-d0e2-45d8-8a48-572c607b7a66
			Logplex-Msg-Count: 2
	*/
	drainToken := uuid.New().String()
	frameID := uuid.New().String()
	req.Header.Set("Content-Type", "application/logplex-1")
	req.Header.Set("Logplex-Drain-Token", fmt.Sprintf("d.%s", drainToken))
	req.Header.Set("Logplex-Frame-Id", frameID)
	req.Header.Set("Logplex-Msg-Count", fmt.Sprintf("%d", len(bodies)))

	return req, nil
}

func TestHerokuDrainTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	type expectedEntry struct {
		labels model.LabelSet
		line   string
	}

	cases := map[string]struct {
		bodies          []string
		expectedEntries []expectedEntry
	}{
		"logplex request with single log line": {
			bodies: []string{testPayload},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"__logplex_host":   "host",
						"__logplex_app":    "heroku",
						"__logplex_proc":   "router",
						"__logplex_log_id": "-",
						"job":              "test_heroku",
					},
					line: `at=info method=GET path="/" host=cryptic-cliffs-27764.herokuapp.com request_id=59da6323-2bc4-4143-8677-cc66ccfb115f fwd="181.167.87.140" dyno=web.1 connect=0ms service=3ms status=200 bytes=6979 protocol=https
`,
				},
			},
		},
		"logplex request with two log lines": {
			bodies: []string{testLogLine1, testLogLine2},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"__logplex_host":   "host",
						"__logplex_app":    "app",
						"__logplex_proc":   "web.1",
						"__logplex_log_id": "-",
						"job":              "test_heroku",
					},
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`,
				},
				{
					labels: model.LabelSet{
						"__logplex_host":   "host",
						"__logplex_app":    "app",
						"__logplex_proc":   "web.1",
						"__logplex_log_id": "-",
						"job":              "test_heroku",
					},
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |      163.92µs |  181.167.87.140 | GET      "/static/main.css"
`,
				},
			},
		},
	}

	//Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", localhost+":0")
	require.NoError(t, err)
	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	require.NoError(t, err)

	// Adjust some of the defaults
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	defaults.HTTPListenAddress = localhost
	defaults.HTTPListenPort = port
	defaults.GRPCListenAddress = localhost
	defaults.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	config := &scrapeconfig.HerokuTargetConfig{
		Server: defaults,
		Labels: model.LabelSet{
			"job": "test_heroku",
		},
	}

	pt, err := NewHerokuTarget(logger, eh, "job2", config)
	require.NoError(t, err)

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Clear received lines after test case is ran
			defer eh.Clear()

			// Send some logs
			ts := time.Now()

			req, err := makeDrainRequest(fmt.Sprintf("http://%s:%d", localhost, port), tc.bodies...)
			require.NoError(t, err, "expected test drain request to be successfully created")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

			// Wait for them to appear in the test handler
			countdown := 1000
			for len(eh.Received()) != 1 && countdown > 0 {
				time.Sleep(1 * time.Millisecond)
				countdown--
			}

			// Make sure we didn't timeout
			require.Equal(t, len(tc.bodies), len(eh.Received()))

			require.Equal(t, len(eh.Received()), len(tc.expectedEntries), "expected to receive equal amount of expected label sets")
			for i, expectedEntry := range tc.expectedEntries {
				// TODO: Add assertion over propagated timestamp
				actualEntry := eh.Received()[i]

				require.Equal(t, expectedEntry.line, actualEntry.Line, "expected line to be equal for %d-th entry", i)

				expectedLS := expectedEntry.labels
				actualLS := actualEntry.Labels
				for label, value := range expectedLS {
					require.Equal(t, expectedLS[label], actualLS[label], "expected label %s to be equal to %s in %d-th entry", label, value, i)
				}

				// Timestamp is always set in the handler, we expect received timestamps to be slightly higher than the timestamp when we started sending logs.
				require.GreaterOrEqual(t, actualEntry.Timestamp.Unix(), ts.Unix(), "expected %d-th entry to have a received timestamp greater than publish time", i)
			}
		})
	}

	_ = pt.Stop()
}
