package p2p

import (
	"context"
	"encoding/json"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
)

type Topic struct {
	topic *pubsub.Topic
	sub   *pubsub.Subscription
	ps    *pubsub.PubSub
}

func topicInit(ctx context.Context, node host.Host) (Topic, error) {
	ps, err := pubsub.NewGossipSub(ctx, node)
	if err != nil {
		return Topic{}, err
	}
	topic, err := ps.Join("test-topic")
	if err != nil {
		return Topic{}, err
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return Topic{}, err
	}
	return Topic{
		topic: topic,
		sub:   sub,
		ps:    ps,
	}, nil
}

func (t Topic) broadcast(message Message, ctx context.Context) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return t.topic.Publish(ctx, data)
}
