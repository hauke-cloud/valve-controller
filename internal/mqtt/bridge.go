package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/hauke-cloud/iot/valve-controller/internal/metrics"
)

// BridgeConfig holds all parameters needed to connect to one Tasmota MQTT bridge.
type BridgeConfig struct {
	// BridgeName is the Tasmota bridge topic prefix (e.g. "tasmota_office").
	BridgeName string
	Host       string
	Port       int32
	Username   string
	Password   string
	// ClientID suffix — full ID is "valve-controller-<hostname>-<BridgeName>".
	Hostname string
	// MaxReconnectBackoff in seconds, 0 → use 60s default.
	MaxReconnectBackoff int32
}

// BridgeClient manages a single MQTT connection for one Tasmota bridge.
type BridgeClient struct {
	cfg        BridgeConfig
	log        *slog.Logger
	metrics    *metrics.Metrics
	dispatcher *Dispatcher
	client     pahomqtt.Client
	mu         sync.Mutex
	connected  bool
}

func newBridgeClient(cfg BridgeConfig, disp *Dispatcher, m *metrics.Metrics, log *slog.Logger) *BridgeClient {
	bc := &BridgeClient{
		cfg:        cfg,
		log:        log.With("bridge", cfg.BridgeName),
		metrics:    m,
		dispatcher: disp,
	}

	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port))
	opts.SetClientID(fmt.Sprintf("valve-controller-%s-%s", cfg.Hostname, cfg.BridgeName))
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	maxBackoff := time.Duration(cfg.MaxReconnectBackoff) * time.Second
	if maxBackoff == 0 {
		maxBackoff = 60 * time.Second
	}
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(maxBackoff)
	opts.SetConnectRetryInterval(time.Second)
	opts.SetConnectionLostHandler(bc.onConnectionLost)
	opts.SetOnConnectHandler(bc.onConnect)
	opts.SetConnectRetry(true)

	bc.client = pahomqtt.NewClient(opts)
	return bc
}

func (bc *BridgeClient) Connect(ctx context.Context) error {
	token := bc.client.Connect()
	select {
	case <-token.Done():
		if token.Error() != nil {
			return fmt.Errorf("connect to %s:%d: %w", bc.cfg.Host, bc.cfg.Port, token.Error())
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (bc *BridgeClient) Disconnect() {
	bc.client.Disconnect(500)
	bc.metrics.MQTTConnectionState.WithLabelValues(bc.cfg.BridgeName).Set(0)
}

func (bc *BridgeClient) onConnect(c pahomqtt.Client) {
	bc.mu.Lock()
	bc.connected = true
	bc.mu.Unlock()
	bc.metrics.MQTTConnectionState.WithLabelValues(bc.cfg.BridgeName).Set(1)
	bc.log.Info("MQTT connected, subscribing to topics")
	bc.resubscribe(c)
}

func (bc *BridgeClient) onConnectionLost(_ pahomqtt.Client, err error) {
	bc.mu.Lock()
	bc.connected = false
	bc.mu.Unlock()
	bc.metrics.MQTTConnectionState.WithLabelValues(bc.cfg.BridgeName).Set(0)
	bc.log.Warn("MQTT connection lost, will reconnect", "err", err)
}

func (bc *BridgeClient) resubscribe(c pahomqtt.Client) {
	sensorTopic := fmt.Sprintf("tele/%s/SENSOR", bc.cfg.BridgeName)
	resultTopic := fmt.Sprintf("stat/%s/RESULT", bc.cfg.BridgeName)

	c.Subscribe(sensorTopic, 1, bc.handleSensor)
	c.Subscribe(resultTopic, 0, bc.handleResult)
}

func (bc *BridgeClient) handleSensor(_ pahomqtt.Client, msg pahomqtt.Message) {
	bridge := BridgeNameFromTopic(msg.Topic())
	bc.metrics.MQTTMessagesReceived.WithLabelValues(bridge).Inc()

	events, err := ParseSensorPayload(bridge, msg.Payload())
	if err != nil {
		bc.log.Debug("failed to parse sensor payload", "err", err)
		return
	}
	for _, ev := range events {
		bc.dispatcher.Dispatch(ev)
	}
}

func (bc *BridgeClient) handleResult(_ pahomqtt.Client, msg pahomqtt.Message) {
	bridge := BridgeNameFromTopic(msg.Topic())
	bc.metrics.MQTTMessagesReceived.WithLabelValues(bridge).Inc()

	ev, err := ParseResultPayload(bridge, msg.Payload())
	if err != nil {
		bc.log.Debug("failed to parse result payload", "err", err)
		return
	}
	if ev.ZbSendDone {
		bc.log.Debug("ZbSend acknowledged by bridge")
	}
}

// SendZbSend publishes a ZbSend command to this bridge for the given device.
func (bc *BridgeClient) SendZbSend(ctx context.Context, deviceName string, power bool) error {
	payload, err := ZbSendPayload(deviceName, power)
	if err != nil {
		return fmt.Errorf("build ZbSend payload: %w", err)
	}
	topic := fmt.Sprintf("cmnd/%s/ZbSend", bc.cfg.BridgeName)
	token := bc.client.Publish(topic, 1, false, payload)
	select {
	case <-token.Done():
		return token.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsConnected reports whether the bridge is currently connected.
func (bc *BridgeClient) IsConnected() bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.connected
}

