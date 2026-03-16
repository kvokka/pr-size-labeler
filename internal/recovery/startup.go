package recovery

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"pr-size-labeler/internal/auth"
	"pr-size-labeler/internal/config"
	"pr-size-labeler/internal/githubapi"
)

type DeliveryClient interface {
	ListAppHookDeliveriesSince(ctx context.Context, cutoff time.Time) ([]githubapi.AppHookDelivery, error)
	RedeliverAppHookDelivery(ctx context.Context, deliveryID int64) error
}

type DeliveryClientFactory func(token string) DeliveryClient

type StartupRecovery struct {
	logger         *log.Logger
	now            func() time.Time
	clientFactory  DeliveryClientFactory
	appTokenSource auth.AppTokenSource
}

func NewStartupRecovery(logger *log.Logger, appTokenSource auth.AppTokenSource, clientFactory DeliveryClientFactory) *StartupRecovery {
	if logger == nil {
		logger = log.Default()
	}
	return &StartupRecovery{
		logger:         logger,
		now:            time.Now,
		clientFactory:  clientFactory,
		appTokenSource: appTokenSource,
	}
}

func (r *StartupRecovery) Run(ctx context.Context, env config.Env) error {
	if !env.StartupFailedDeliveryRecoveryEnabled {
		r.logger.Printf("startup_failed_delivery_recovery enabled=false")
		return nil
	}
	cutoff := r.now().UTC().Add(-env.StartupFailedDeliveryRecoveryLookback)
	if env.LogPrivateDetails {
		r.logger.Printf("startup_failed_delivery_recovery enabled=true lookback=%s cutoff=%s", env.StartupFailedDeliveryRecoveryLookback, cutoff.Format(time.RFC3339))
	} else {
		r.logger.Printf("startup_failed_delivery_recovery enabled=true")
	}
	appToken, err := r.appTokenSource.AppToken(ctx)
	if err != nil {
		return fmt.Errorf("create app token for startup recovery: %w", err)
	}
	client := r.clientFactory(appToken)
	deliveries, err := client.ListAppHookDeliveriesSince(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("list app hook deliveries: %w", err)
	}
	failed := 0
	redelivered := 0
	failedRedeliveries := 0
	for _, delivery := range deliveries {
		if strings.EqualFold(delivery.Status, "OK") {
			continue
		}
		failed++
		if env.LogPrivateDetails {
			r.logger.Printf("startup_failed_delivery_recovery delivery_id=%d event=%q action=%q status=%q delivered_at=%s redelivery=%t attempting_redelivery=true", delivery.ID, delivery.Event, delivery.Action, delivery.Status, delivery.DeliveredAt.Format(time.RFC3339), delivery.Redelivery)
		}
		if err := client.RedeliverAppHookDelivery(ctx, delivery.ID); err != nil {
			failedRedeliveries++
			if env.LogPrivateDetails {
				r.logger.Printf("startup_failed_delivery_recovery delivery_id=%d redelivery_success=false error=%v", delivery.ID, err)
			} else {
				r.logger.Printf("startup_failed_delivery_recovery redelivery_success=false")
			}
			continue
		}
		redelivered++
		if env.LogPrivateDetails {
			r.logger.Printf("startup_failed_delivery_recovery delivery_id=%d redelivery_success=true", delivery.ID)
		} else {
			r.logger.Printf("startup_failed_delivery_recovery redelivery_success=true")
		}
	}
	r.logger.Printf("startup_failed_delivery_recovery summary listed=%d failed=%d redelivered=%d redelivery_failures=%d", len(deliveries), failed, redelivered, failedRedeliveries)
	return nil
}
