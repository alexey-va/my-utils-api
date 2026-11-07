package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/alexey-va/my-utils-api/internal/businessbot"
	"github.com/alexey-va/my-utils-api/internal/config"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/store"
	"github.com/alexey-va/my-utils-api/internal/telegram"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func configureBusinessTelegram(cfg config.Config, proxy *url.URL, users *store.UserStore, service *business.Service, model *openrouter.Client, redisClient *redis.Client) (*telegram.Runner, *businessbot.Notifications, error) {
	token := strings.TrimSpace(os.Getenv("BUSINESS_TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return nil, nil, nil
	}
	owner, ownerErr := uuid.Parse(os.Getenv("BUSINESS_WORKSPACE_OWNER_ID"))
	userID, userErr := strconv.ParseInt(os.Getenv("BUSINESS_TELEGRAM_USER_ID"), 10, 64)
	if ownerErr != nil || owner == uuid.Nil || userErr != nil || userID <= 0 || model == nil {
		return nil, nil, errors.New("business Telegram requires a workspace owner, an allowed Telegram user and OpenRouter")
	}
	if token == strings.TrimSpace(cfg.Telegram.BotToken) || token == strings.TrimSpace(cfg.VPNTelegram.BotToken) {
		return nil, nil, errors.New("business Telegram requires its own bot token")
	}
	ready := func(ctx context.Context) bool {
		user, err := users.FindByID(ctx, owner.String())
		return err == nil && user.Role == "ADMIN" && !user.MustChangePassword
	}
	client := telegram.NewClient(token, "https://api.telegram.org", proxy)
	assistant := businessbot.NewAssistant(model, service)
	dispatcher := businessbot.NewTelegram(owner.String(), userID, ready, client, service, assistant)
	runner := telegram.NewRunner(client, dispatcher, true)
	notifications := businessbot.NewNotifications(service, redisClient, owner.String(), dispatcher.Send, ready)
	return runner, notifications, nil
}
