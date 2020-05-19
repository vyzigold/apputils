package tests

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/infrawatch/apputils/config"
	"github.com/infrawatch/apputils/connector"
	"github.com/infrawatch/apputils/logging"
	"github.com/stretchr/testify/assert"
)

const (
	QDRMsg        = "{\"message\": \"smart gateway test\"}"
	ConfigContent = `{
	"Amqp1": {
		"Connection": {
			"Address": "amqp://127.0.0.1:5672/collectd/telemetry",
		  "SendTimeout": 2
		},
		"Client": {
			"Name": "connectortest"
		}
	}
}
`
)

type MockedConnection struct {
	Address     string
	SendTimeout int
}

type MockedClient struct {
	Name string
}

func TestAMQP10SendAndReceiveMessage(t *testing.T) {
	tmpdir, err := ioutil.TempDir(".", "connector_test_tmp")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)
	logpath := path.Join(tmpdir, "test.log")
	logger, err := logging.NewLogger(logging.DEBUG, logpath)
	if err != nil {
		t.Fatalf("Failed to open log file %s. %s\n", logpath, err)
	}
	defer logger.Destroy()

	metadata := map[string][]config.Parameter{
		"Amqp1": []config.Parameter{
			config.Parameter{Name: "LogFile", Tag: ``, Default: logpath, Validators: []config.Validator{}},
		},
	}
	cfg := config.NewJSONConfig(metadata, logger)
	cfg.AddStructured("Amqp1", "Client", ``, MockedClient{})
	cfg.AddStructured("Amqp1", "Connection", ``, MockedConnection{})

	err = cfg.ParseBytes([]byte(ConfigContent))
	if err != nil {
		t.Fatalf("Failed to parse config file: %s", err)
	}

	conn, err := connector.NewAMQP10Connector(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to connect to QDR: %s", err)
	}
	conn.Connect()
	err = conn.CreateReceiver("qdrtest", -1)
	if err != nil {
		t.Fatalf("Failed to create receiver: %s", err)
	}

	receiver := make(chan interface{})
	sender := make(chan interface{})
	conn.Start(receiver, sender)

	t.Run("Test receive", func(t *testing.T) {
		t.Parallel()
		data := <-receiver
		assert.Equal(t, QDRMsg, (data.(connector.AMQP10Message)).Body)
	})
	t.Run("Test send and ACK", func(t *testing.T) {
		t.Parallel()
		sender <- connector.AMQP10Message{Address: "qdrtest", Body: QDRMsg}
	})
}

func TestLoki(t *testing.T) {
	// TODO: read these from config
	server := "http://localhost"
	port := "3100"
	batchSize := 4
	maxWaitTime := 50 * time.Millisecond
	testId := strconv.FormatInt(time.Now().UnixNano(), 16)
	url := strings.Join([]string{server, port}, ":")

	client, err := connector.NewLokiConnector(url, batchSize, maxWaitTime)
	if err != nil {
		t.Fatalf("Failed to create loki client: %s", err)
	}
	assert.Equal(t, client.IsReady(), true, "The client isn't ready")

	defer func() {
		client.Shutdown()
	}()

	// push a whole batch
	t.Run("Test sending in batches", func(t *testing.T) {
		c, err := connector.NewLokiConnector(url, batchSize, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to create loki client: %s", err)
		}
		c.Start()
		defer func() {
			c.Shutdown()
		}()

		currentTime := time.Duration(time.Now().UnixNano())
		for i := 0; i < batchSize; i++ {
			labels := make(map[string]string)
			labels["test"] = "batch"
			labels["unique"] = testId
			labels["order"] = strconv.FormatInt(int64(i), 10)
			message := connector.Message{
				Time:    currentTime,
				Message: "test message batch",
			}
			messages := []connector.Message{message}
			c.AddStream(labels, messages)
		}
		time.Sleep(10 * time.Millisecond)

		// query it back
		queryString := "{test=\"batch\",unique=\"" + testId + "\"}"
		answer, err := c.Query(queryString, 0, batchSize)
		if err != nil {
			t.Fatalf("Couldn't query loki after batch push: %s", err)
		}
		assert.Equal(t, batchSize, len(answer), "Query after batch test returned wrong count of results")
		for _, message := range answer {
			assert.Equal(t, "test message batch", message.Message, "Wrong test message when querying for batch test results")
			assert.Equal(t, currentTime, message.Time, "Wrong timestamp in queried batch test message")
		}
	})

	// push just one message and wait for the maxWaitTime to pass
	t.Run("Test waiting for maxWaitTime to pass", func(t *testing.T) {
		c, err := connector.NewLokiConnector(url, batchSize, maxWaitTime)
		if err != nil {
			t.Fatalf("Failed to create loki client: %s", err)
		}
		c.Start()
		defer func() {
			c.Shutdown()
		}()

		labels := make(map[string]string)
		labels["test"] = "single"
		labels["unique"] = testId
		currentTime := time.Duration(time.Now().UnixNano())
		message := connector.Message{
			Time:    currentTime,
			Message: "test message single",
		}
		messages := []connector.Message{message}
		c.AddStream(labels, messages)
		time.Sleep(80 * time.Millisecond)

		// query it back
		queryString := "{test=\"single\",unique=\"" + testId + "\"}"
		answer, err := c.Query(queryString, 0, batchSize)
		if err != nil {
			t.Fatalf("Couldn't query loki after testing maxWaitTime: %s", err)
		}
		assert.Equal(t, 1, len(answer), "Query after maxWaitTime test returned wrong count of results")
		for _, message := range answer {
			assert.Equal(t, "test message single", message.Message, "Wrong test message when querying for maxWaitTime test results")
			assert.Equal(t, currentTime, message.Time, "Wrong timestamp in queried maxWaitTime test message")
		}
	})

	// test sending multiple messages in a single stream
	t.Run("Test sending multiple messages in a single stream", func(t *testing.T) {
		c, err := connector.NewLokiConnector(url, batchSize, maxWaitTime)
		if err != nil {
			t.Fatalf("Failed to create loki client: %s", err)
		}
		c.Start()
		defer func() {
			c.Shutdown()
		}()

		labels := make(map[string]string)
		labels["test"] = "multiple_in_a_stream"
		labels["unique"] = testId
		currentTime := time.Duration(time.Now().UnixNano())
		var messages []connector.Message
		for i := 0; i < 2; i++ {
			message := connector.Message{
				Time:    currentTime,
				Message: strconv.FormatInt(int64(i), 10),
			}
			messages = append(messages, message)
		}
		c.AddStream(labels, messages)
		time.Sleep(80 * time.Millisecond)

		// query it back
		queryString := "{test=\"multiple_in_a_stream\",unique=\"" + testId + "\"}"
		answer, err := c.Query(queryString, 0, batchSize)
		if err != nil {
			t.Fatalf("Couldn't query loki after pushing multiple messages in a stream: %s", err)
		}
		assert.Equal(t, 2, len(answer), "Query after sending multiple messages in a single stream returned wrong count of results")
		// we should get one message, that equals "0" and one
		// message, that equals "1", but we don't know in
		// which order
		if (answer[0].Message != "0" || answer[1].Message != "1") &&
			(answer[0].Message != "1" || answer[1].Message != "0") {
			t.Fatalf("Wrong test message when querying for \"send multiple messages in a single stream\" results")
			assert.Equal(t, currentTime, answer[0].Time, "Wrong timestamp in queried \"send multiple messages in a single stream\"test message")
			assert.Equal(t, currentTime, answer[1].Time, "Wrong timestamp in queried \"send multiple messages in a single stream\"test message")
		}
	})
}
