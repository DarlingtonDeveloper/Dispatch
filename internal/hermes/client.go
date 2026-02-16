package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client interface {
	Publish(subject string, data interface{}) error
	Subscribe(subject string, handler func(subject string, data []byte)) error
	Close()
}

type NATSClient struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	subs   []*nats.Subscription
	logger *slog.Logger
}

func NewNATSClient(ctx context.Context, url string, logger *slog.Logger) (*NATSClient, error) {
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(60),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}

	c := &NATSClient{conn: nc, js: js, logger: logger}
	if err := c.ensureStream(ctx); err != nil {
		logger.Warn("failed to ensure stream", "error", err)
	}
	return c, nil
}

func (c *NATSClient) ensureStream(ctx context.Context) error {
	maxAge, _ := time.ParseDuration(StreamMaxAge)
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{"swarm.task.>", "swarm.dispatch.>", "swarm.backlog.>", "swarm.override.>"},
		MaxAge:   maxAge,
	})
	return err
}

func (c *NATSClient) Publish(subject string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.conn.Publish(subject, payload)
}

func (c *NATSClient) Subscribe(subject string, handler func(string, []byte)) error {
	sub, err := c.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Subject, msg.Data)
	})
	if err != nil {
		return err
	}
	c.subs = append(c.subs, sub)
	return nil
}

func (c *NATSClient) Close() {
	for _, sub := range c.subs {
		_ = sub.Unsubscribe()
	}
	c.conn.Close()
}
