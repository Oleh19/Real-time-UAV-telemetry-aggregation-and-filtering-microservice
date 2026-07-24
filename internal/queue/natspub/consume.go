package natspub

import "github.com/nats-io/nats.go/jetstream"

func ConsumeAll(consumers []jetstream.Consumer, handler jetstream.MessageHandler) (func(), error) {
	contexts := make([]jetstream.ConsumeContext, 0, len(consumers))
	stop := func() {
		for _, cc := range contexts {
			cc.Stop()
		}
	}
	for _, consumer := range consumers {
		cc, err := consumer.Consume(handler)
		if err != nil {
			stop()
			return nil, err
		}
		contexts = append(contexts, cc)
	}
	return stop, nil
}
