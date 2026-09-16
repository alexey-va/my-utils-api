package temporal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	sdkclient "go.temporal.io/sdk/client"
	sdkmocks "go.temporal.io/sdk/mocks"
)

func TestStartAgentTurnSignalsStablePerChatWorkflow(t *testing.T) {
	client := &sdkmocks.Client{}
	input := AgentTurnInput{ChatID: 42, UserID: 7, Text: "hello"}
	var gotWorkflow interface{}
	var gotOptions sdkclient.StartWorkflowOptions
	client.On("SignalWithStartWorkflow", mock.Anything, AgentTurnQueueWorkflowID(input.ChatID), AgentTurnQueueSignal, input, mock.Anything, mock.Anything, AgentTurnQueueInput{}).
		Run(func(args mock.Arguments) {
			gotOptions = args.Get(4).(sdkclient.StartWorkflowOptions)
			gotWorkflow = args.Get(5)
		}).Return(nil, nil).Once()

	service := &Service{config: ServiceConfig{TaskQueue: "myutils-go-v1"}, client: client}
	id, err := service.StartAgentTurn(context.Background(), input)
	if err != nil {
		t.Fatalf("StartAgentTurn error = %v", err)
	}
	if id != "go-v1-agent-turn-queue-42" {
		t.Fatalf("workflow id = %q", id)
	}
	if gotOptions.ID != id || gotOptions.TaskQueue != "myutils-go-v1" {
		t.Fatalf("start options = %#v", gotOptions)
	}
	if gotWorkflow == nil {
		t.Fatal("queue workflow was not supplied")
	}
	client.AssertExpectations(t)
}

func TestSendWeeklyReportNowStartsDisposableWorkflowForRuntimeDate(t *testing.T) {
	client := &sdkmocks.Client{}
	now := time.Date(2026, 9, 16, 22, 30, 0, 0, time.UTC)
	wantInput := WeeklyReportActivityInput{ChatID: 42, ReportDate: "2026-09-17", LookbackDays: 90}
	var gotOptions sdkclient.StartWorkflowOptions
	var gotWorkflow interface{}
	client.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, wantInput).
		Run(func(args mock.Arguments) {
			gotOptions = args.Get(1).(sdkclient.StartWorkflowOptions)
			gotWorkflow = args.Get(2)
		}).Return(nil, nil).Once()

	service := &Service{
		config: ServiceConfig{TaskQueue: "myutils-go-v1", ZoneID: func() string { return "Europe/Moscow" }},
		client: client,
		now:    func() time.Time { return now },
	}
	receipt, err := service.SendWeeklyReportNow(context.Background(), 42)
	if err != nil {
		t.Fatalf("SendWeeklyReportNow error = %v", err)
	}
	if gotOptions.TaskQueue != "myutils-go-v1" || gotOptions.ID == "" {
		t.Fatalf("start options = %#v", gotOptions)
	}
	if gotWorkflow == nil || receipt == "" {
		t.Fatalf("workflow = %#v, receipt = %q", gotWorkflow, receipt)
	}
	client.AssertExpectations(t)
}
