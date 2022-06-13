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

func TestPlaintextPushTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	//Create PushTarget
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

	config := &scrapeconfig.HerokuDrainTargetConfig{
		Server: defaults,
		Labels: model.LabelSet{
			"job": "test_heroku",
		},
	}

	pt, err := NewHerokuDrainTarget(logger, eh, "job2", config)
	require.NoError(t, err)

	// Send some logs
	ts := time.Now()

	req, err := makeDrainRequest(fmt.Sprintf("http://%s:%d", localhost, port), testPayload)
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
	require.Equal(t, 1, len(eh.Received()))

	expectedParsedTime, _ := time.Parse(time.RFC3339, "2022-06-13T14:52:23.622778+00:00")

	// Spot check the first value in the result to make sure relabel rules were applied properly
	receivedLabels := eh.Received()[0].Labels
	require.Equal(t, model.LabelValue("host"), receivedLabels["__logplex_host"])
	require.Equal(t, model.LabelValue("heroku"), receivedLabels["__logplex_app"])
	require.Equal(t, model.LabelValue("router"), receivedLabels["__logplex_proc"])
	require.Equal(t, model.LabelValue("-"), receivedLabels["__logplex_log_id"])
	require.Equal(t, model.LabelValue("test_heroku"), receivedLabels["job"])
	gotTime, _ := time.Parse(time.RFC3339, string(receivedLabels["__logplex_ts"]))
	require.GreaterOrEqual(t, gotTime, expectedParsedTime)

	// Timestamp is always set in the handler, we expect received timestamps to be slightly higher than the timestamp when we started sending logs.
	require.GreaterOrEqual(t, eh.Received()[0].Timestamp.Unix(), ts.Unix())

	_ = pt.Stop()
}
