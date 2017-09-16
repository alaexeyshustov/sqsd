package sqsd

import (
	"context"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"strconv"
	"testing"
)

func TestNewWorker(t *testing.T) {
	c := &SQSDConf{}
	r := &SQSResource{}
	w := NewWorker(r, c)
	if w == nil {
		t.Error("worker not loaded")
	}
}

func TestSetupJob(t *testing.T) {
	c := &SQSDConf{}
	r := &SQSResource{}
	w := NewWorker(r, c)

	sqsMsg := &sqs.Message{
		MessageId: aws.String("foobar"),
		Body:      aws.String(`{"from":"user_1","to":"room_1","msg":"Hello!"}`),
	}

	job := w.SetupJob(sqsMsg)
	if job == nil {
		t.Error("job not created")
	}

	if _, exists := w.CurrentWorkings[job.ID()]; !exists {
		t.Error("job not registered")
	}

	delete(w.CurrentWorkings, job.ID())
	if _, exists := w.CurrentWorkings[job.ID()]; exists {
		t.Error("job not deleted")
	}
}

func TestIsRunnable(t *testing.T) {
	c := &SQSDConf{ProcessCount: 5}
	r := &SQSResource{}
	w := NewWorker(r, c)

	w.Runnable = false
	if w.IsWorkerAvailable() {
		t.Error("IsWorkerAvailable flag is wrong")
	}
	w.Runnable = true
	if !w.IsWorkerAvailable() {
		t.Error("IsWorkerAvailable flag is wrong")
	}

	for i := 1; i <= w.ProcessCount-1; i++ {
		w.SetupJob(&sqs.Message{
			MessageId: aws.String("id:" + strconv.Itoa(i)),
			Body:      aws.String(`{"from":"user_1","to":"room_1","msg":"Hello!"}`),
		})
		if !w.IsWorkerAvailable() {
			t.Errorf("flag disabled! idx: %d", i)
		}
	}

	w.SetupJob(&sqs.Message{
		MessageId: aws.String("id:" + strconv.Itoa(w.ProcessCount)),
		Body:      aws.String(`{"from":"user_1","to":"room_1","msg":"Hello!"}`),
	})
	if w.IsWorkerAvailable() {
		t.Errorf("flag disabled! idx: %d", w.ProcessCount)
	}
}

func TestCanWork(t *testing.T) {
	c := &SQSDConf{ProcessCount: 5}
	r := &SQSResource{}
	w := NewWorker(r, c)

	sqsMsg := &sqs.Message{
		MessageId: aws.String("id:100000"),
		Body:      aws.String(`{"from":"user_1","to":"room_1","msg":"Hello!"}`),
	}

	w.Runnable = false
	if w.CanWork(sqsMsg) {
		t.Error("CanWork not working")
	}

	w.Runnable = true
	if !w.CanWork(sqsMsg) {
		t.Error("CanWork not working")
	}
	w.SetupJob(sqsMsg)
	if w.CanWork(sqsMsg) {
		t.Error("Canwork not working")
	}
}

func TestHandleMessage(t *testing.T) {
	c := &SQSDConf{ProcessCount: 5}
	r := &SQSResource{Client: &SQSMockClient{}}
	w := NewWorker(r, c)

	ctx := context.Background()

	ts := MockJobServer()
	defer ts.Close()

	t.Run("job failed", func(t *testing.T) {
		job := w.SetupJob(&sqs.Message{
			MessageId:     aws.String("TestHandleMessageNG"),
			Body:          aws.String(`{"hoge":"fuga"}`),
			ReceiptHandle: aws.String("aaaaaaaaaa"),
		})
		job.URL = ts.URL + "/error"

		go w.HandleMessage(ctx, job)
		<-job.Done()
		if _, exists := w.CurrentWorkings[job.ID()]; exists {
			t.Error("working job yet exists")
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		job := w.SetupJob(&sqs.Message{
			MessageId:     aws.String("TestHandleMessageErr"),
			Body:          aws.String(`{"hoge":"fuga"}`),
			ReceiptHandle: aws.String("aaaaaaaaaa"),
		})
		job.URL = ts.URL + "/long"
		parent, cancel := context.WithCancel(ctx)
		go w.HandleMessage(parent, job)
		cancel()
		<-job.Done()
		if _, exists := w.CurrentWorkings[job.ID()]; exists {
			t.Error("working job yet exists")
		}
	})

	t.Run("success", func(t *testing.T) {
		job := w.SetupJob(&sqs.Message{
			MessageId:     aws.String("TestHandleMessageOK"),
			Body:          aws.String(`{"hoge":"fuga"}`),
			ReceiptHandle: aws.String("aaaaaaaaaa"),
		})
		job.URL = ts.URL + "/ok"
		go w.HandleMessage(ctx, job)
		<-job.Done()
		if _, exists := w.CurrentWorkings[job.ID()]; exists {
			t.Error("working job yet exists")
		}
	})
}