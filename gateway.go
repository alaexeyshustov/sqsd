package sqsd

import (
	"context"
	"sync"
	"time"

	"github.com/AsynkronIT/protoactor-go/log"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/sqs"
	"golang.org/x/sync/semaphore"

	"github.com/taiyoh/sqsd/locker"
	nooplocker "github.com/taiyoh/sqsd/locker/noop"
)

// Gateway fetches and removes jobs from SQS.
type Gateway struct {
	parallel int
	queueURL string
	queue    *sqs.SQS
	timeout  int64
}

// GatewayParameter sets parameter in Gateway.
type GatewayParameter func(*Gateway)

// GatewayParallel sets parallel size in Gateway.
func GatewayParallel(p int) GatewayParameter {
	return func(g *Gateway) {
		g.parallel = p
	}
}

// GatewayVisibilityTimeout sets visibility timeout in Gateway to receive messages from SQS.
func GatewayVisibilityTimeout(d time.Duration) GatewayParameter {
	return func(g *Gateway) {
		g.timeout = int64(d.Seconds())
	}
}

// NewGateway returns Gateway object.
func NewGateway(queue *sqs.SQS, qURL string, fns ...GatewayParameter) *Gateway {
	gw := &Gateway{
		queueURL: qURL,
		queue:    queue,
		parallel: 1,
		timeout:  30, // default SQS settings
	}
	for _, fn := range fns {
		fn(gw)
	}
	return gw
}

type fetcher struct {
	distributorInterval time.Duration
	fetcherInterval     time.Duration
	queueURL            string
	queue               *sqs.SQS
	distributorCh       chan Message
	timeout             int64
	numberOfMessages    int64
	locker              locker.QueueLocker
}

// FetcherParameter sets parameter to fetcher by functional option pattern.
type FetcherParameter func(*fetcher)

// FetcherDistributorInterval sets interval duration of distributor request to fetcher.
// Fetcher watches distributor status because
// fetcher should be stopped when messages which distributor has is over capacity.
func FetcherDistributorInterval(d time.Duration) FetcherParameter {
	return func(f *fetcher) {
		f.distributorInterval = d
	}
}

// FetcherInterval sets interval duration of receiving queue request to fetcher.
func FetcherInterval(d time.Duration) FetcherParameter {
	return func(f *fetcher) {
		f.fetcherInterval = d
	}
}

// FetcherQueueLocker sets QueueLocker in Fetcher to block duplicated queue.
func FetcherQueueLocker(l locker.QueueLocker) FetcherParameter {
	return func(f *fetcher) {
		f.locker = l
	}
}

// FetcherMaxMessages sets MaxNumberOfMessages of SQS between 1 and 10.
// Fetcher's default value is 10.
// if supplied value is out of range, forcely sets 1 or 10.
// (if n is less than 1, set 1 and is more than 10, set 10)
func FetcherMaxMessages(n int64) FetcherParameter {
	if n < 1 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	return func(f *fetcher) {
		f.numberOfMessages = n
	}
}

// StartFetcher starts Fetcher to fetch sqs messages.
func (g *Gateway) StartFetcher(ctx context.Context, distributor chan Message, fns ...FetcherParameter) {
	f := &fetcher{
		distributorCh:       distributor,
		queue:               g.queue,
		queueURL:            g.queueURL,
		timeout:             g.timeout,
		distributorInterval: time.Second,
		fetcherInterval:     100 * time.Millisecond,
		numberOfMessages:    10,
		locker:              nooplocker.Get(),
	}
	for _, fn := range fns {
		fn(f)
	}

	var wg sync.WaitGroup
	wg.Add(g.parallel)
	for i := 0; i < g.parallel; i++ {
		go f.RunForFetch(ctx, &wg)
	}
	wg.Wait()
}

func (f *fetcher) RunForFetch(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			messages, err := f.fetch(ctx)
			if err != nil {
				if e, ok := err.(awserr.Error); ok && e.OrigErr() == context.Canceled {
					return
				}
				logger.Error("failed to fetch from SQS", log.Error(err))
			}
			logger.Debug("caught messages.", log.Int("length", len(messages)))
			for _, msg := range messages {
				f.distributorCh <- msg
			}
			time.Sleep(f.fetcherInterval)
		}
	}
}

func (f *fetcher) fetch(ctx context.Context) ([]Message, error) {
	out, err := f.queue.ReceiveMessageWithContext(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &f.queueURL,
		MaxNumberOfMessages: &f.numberOfMessages,
		WaitTimeSeconds:     aws.Int64(20),
		VisibilityTimeout:   aws.Int64(f.timeout),
	})
	if err != nil {
		return nil, err
	}
	receivedAt := time.Now().UTC()
	messages := make([]Message, 0, len(out.Messages))
	for _, msg := range out.Messages {
		if err := f.locker.Lock(ctx, *msg.MessageId); err != nil {
			if err == locker.ErrQueueExists {
				logger.Warn("received message is duplicated", log.String("message_id", *msg.MessageId))
				continue
			}
			return nil, err
		}
		messages = append(messages, Message{
			ID:           *msg.MessageId,
			Payload:      *msg.Body,
			Receipt:      *msg.ReceiptHandle,
			ReceivedAt:   receivedAt,
			ResultStatus: NotRequested,
		})
	}
	return messages, nil
}

type remover struct {
	queue    *sqs.SQS
	queueURL string
	timeout  int64
}

// StartRemover starts remover to remove sqs message.
func (g *Gateway) StartRemover(ctx context.Context, removerCh chan *removeQueueMessage) {
	r := &remover{
		queue:    g.queue,
		queueURL: g.queueURL,
		timeout:  g.timeout,
	}
	sw := semaphore.NewWeighted(int64(g.parallel))
	for msg := range removerCh {
		if err := sw.Acquire(ctx, 1); err != nil {
			return
		}
		go func(msg *removeQueueMessage) {
			defer sw.Release(1)
			r.RunForRemove(ctx, msg)
		}(msg)
	}
}

// removeQueueMessage brings Queue to remove from SQS.
type removeQueueMessage struct {
	SenderCh chan removeQueueResultMessage
	Message  Message
}

// removeQueueResultMessage is message for deleting message from SQS.
type removeQueueResultMessage struct {
	Queue Message
	Err   error
}

func (r *remover) RunForRemove(ctx context.Context, msg *removeQueueMessage) {
	var err error
	for i := 0; i < 16; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		_, err = r.queue.DeleteMessageWithContext(ctx, &sqs.DeleteMessageInput{
			QueueUrl:      &r.queueURL,
			ReceiptHandle: &msg.Message.Receipt,
		})
		cancel()
		if err == nil {
			logger.Debug("succeeded to remove message.", log.String("message_id", msg.Message.ID))
			msg.SenderCh <- removeQueueResultMessage{Queue: msg.Message}
			return
		}
		time.Sleep(time.Second)
	}
	msg.SenderCh <- removeQueueResultMessage{Err: err, Queue: msg.Message}
}
