package temporal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type ServiceConfig struct {
	Target         string
	Namespace      string
	TaskQueue      string
	AllowedChatIDs []int64
	ZoneID         func() string
	ReminderOn     func() bool
	ReminderHour   func() int
	ReminderMinute func() int
}

type Service struct {
	config     ServiceConfig
	activities *Activities
	mu         sync.RWMutex
	client     client.Client
	worker     worker.Worker
}

func NewService(config ServiceConfig, activities *Activities) *Service {
	return &Service{config: config, activities: activities}
}

func (s *Service) Name() string { return "temporal-worker" }

func (s *Service) Warm(ctx context.Context) error {
	temporalClient, err := client.Dial(client.Options{HostPort: s.config.Target, Namespace: s.config.Namespace})
	if err != nil {
		return fmt.Errorf("connect Temporal: %w", err)
	}
	workerInstance := worker.New(temporalClient, s.config.TaskQueue, worker.Options{})
	workerInstance.RegisterWorkflowWithOptions(NotificationWorkflow, workflow.RegisterOptions{Name: "GoV1NotificationWorkflow"})
	workerInstance.RegisterWorkflowWithOptions(EveningReminderWorkflow, workflow.RegisterOptions{Name: "GoV1EveningReminderWorkflow"})
	workerInstance.RegisterWorkflowWithOptions(WeeklyReportWorkflow, workflow.RegisterOptions{Name: "GoV1WeeklyReportWorkflow"})
	workerInstance.RegisterWorkflowWithOptions(AgentTurnWorkflow, workflow.RegisterOptions{Name: "GoV1AgentTurnWorkflow"})
	workerInstance.RegisterActivityWithOptions(s.activities.SendTelegramMessage, activity.RegisterOptions{Name: SendTelegramMessageActivity})
	workerInstance.RegisterActivityWithOptions(s.activities.HasWorkoutToday, activity.RegisterOptions{Name: HasWorkoutTodayActivity})
	workerInstance.RegisterActivityWithOptions(s.activities.SendEveningReminder, activity.RegisterOptions{Name: SendEveningReminderActivity})
	workerInstance.RegisterActivityWithOptions(s.activities.SendWeeklyReport, activity.RegisterOptions{Name: SendWeeklyReportActivity})
	workerInstance.RegisterActivityWithOptions(s.activities.RunAgentTurn, activity.RegisterOptions{Name: RunAgentTurnActivity})
	if err := workerInstance.Start(); err != nil {
		temporalClient.Close()
		return fmt.Errorf("start Temporal worker: %w", err)
	}
	s.mu.Lock()
	s.client, s.worker = temporalClient, workerInstance
	s.mu.Unlock()
	if err := s.ensureRecurring(ctx); err != nil {
		s.Close()
		return err
	}
	slog.InfoContext(ctx, "Temporal Go worker ready", "taskQueue", s.config.TaskQueue, "namespace", s.config.Namespace)
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	workerInstance, clientInstance := s.worker, s.client
	s.worker, s.client = nil, nil
	s.mu.Unlock()
	// Stop outside the mutex: a draining activity may call back into this service.
	if workerInstance != nil {
		workerInstance.Stop()
	}
	if clientInstance != nil {
		clientInstance.Close()
	}
}

func (s *Service) ensureRecurring(ctx context.Context) error {
	zone := s.config.ZoneID()
	for _, chatID := range s.config.AllowedChatIDs {
		if s.config.ReminderOn() {
			_, err := s.start(ctx, client.StartWorkflowOptions{ID: EveningReminderWorkflowID(chatID), TaskQueue: s.config.TaskQueue}, EveningReminderWorkflow,
				ReminderInput{ChatID: chatID, ZoneID: zone, Hour: s.config.ReminderHour(), Minute: s.config.ReminderMinute()})
			if err != nil && !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
				return fmt.Errorf("ensure evening reminder for %d: %w", chatID, err)
			}
		}
		_, err := s.start(ctx, client.StartWorkflowOptions{ID: WeeklyReportWorkflowID(chatID), TaskQueue: s.config.TaskQueue}, WeeklyReportWorkflow,
			WeeklyReportInput{ChatID: chatID, ZoneID: zone, LookbackDays: 90})
		if err != nil && !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
			return fmt.Errorf("ensure weekly report for %d: %w", chatID, err)
		}
	}
	return nil
}

func (s *Service) RefreshEveningReminders(ctx context.Context) error {
	clientInstance, err := s.readyClient()
	if err != nil {
		return err
	}
	for _, chatID := range s.config.AllowedChatIDs {
		workflowID := EveningReminderWorkflowID(chatID)
		if s.config.ReminderOn() {
			_, err := clientInstance.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflowID, TaskQueue: s.config.TaskQueue}, EveningReminderWorkflow,
				ReminderInput{ChatID: chatID, ZoneID: s.config.ZoneID(), Hour: s.config.ReminderHour(), Minute: s.config.ReminderMinute()})
			if err != nil && !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
				return err
			}
		} else if err := clientInstance.CancelWorkflow(ctx, workflowID, ""); err != nil {
			var notFound *serviceerror.NotFound
			if !errors.As(err, &notFound) {
				return err
			}
		}
	}
	return nil
}

func (s *Service) SendNow(ctx context.Context, chatID int64, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("текст уведомления пустой")
	}
	id := NotificationWorkflowID(chatID)
	_, err := s.start(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: s.config.TaskQueue}, NotificationWorkflow,
		NotificationInput{ChatID: chatID, Message: message, DeliverAtEpochMillis: time.Now().UnixMilli()})
	if err != nil {
		return "", err
	}
	return "Уведомление отправляется сейчас (workflow " + id + ").", nil
}

func (s *Service) Schedule(ctx context.Context, chatID int64, message, deliverAtRaw string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", errors.New("текст уведомления пустой")
	}
	deliverAt, err := parseDeliveryTime(deliverAtRaw, s.config.ZoneID())
	if err != nil {
		return "", err
	}
	if !deliverAt.After(time.Now()) {
		return "", errors.New("deliver_at должен быть в будущем")
	}
	id := NotificationWorkflowID(chatID)
	_, err = s.start(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: s.config.TaskQueue}, NotificationWorkflow,
		NotificationInput{ChatID: chatID, Message: message, DeliverAtEpochMillis: deliverAt.UnixMilli()})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Напоминание запланировано на %s (workflow %s).", deliverAt.Format("02.01.2006 15:04"), id), nil
}

func (s *Service) Cancel(ctx context.Context, workflowID string) (bool, error) {
	clientInstance, err := s.readyClient()
	if err != nil {
		return false, err
	}
	err = clientInstance.CancelWorkflow(ctx, strings.TrimSpace(workflowID), "")
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) StartAgentTurn(ctx context.Context, input AgentTurnInput) (string, error) {
	id := AgentTurnWorkflowID(input.ChatID)
	_, err := s.start(ctx, client.StartWorkflowOptions{ID: id, TaskQueue: s.config.TaskQueue}, AgentTurnWorkflow, input)
	return id, err
}

func (s *Service) start(ctx context.Context, options client.StartWorkflowOptions, workflowFunction any, args ...any) (client.WorkflowRun, error) {
	clientInstance, err := s.readyClient()
	if err != nil {
		return nil, err
	}
	return clientInstance.ExecuteWorkflow(ctx, options, workflowFunction, args...)
}

func (s *Service) readyClient() (client.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.client == nil {
		return nil, errors.New("Temporal worker is not ready")
	}
	return s.client, nil
}

func parseDeliveryTime(raw, zoneID string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value, nil
	}
	location, err := time.LoadLocation(zoneID)
	if err != nil {
		return time.Time{}, fmt.Errorf("неверный часовой пояс %s", zoneID)
	}
	value, err := time.ParseInLocation("2006-01-02T15:04:05", raw, location)
	if err != nil {
		return time.Time{}, errors.New("неверный deliver_at: нужен ISO datetime")
	}
	return value, nil
}
