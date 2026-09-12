package temporal

import (
	"context"
	"testing"

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
