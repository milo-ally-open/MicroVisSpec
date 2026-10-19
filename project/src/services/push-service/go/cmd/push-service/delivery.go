package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	deliveryLeaseDuration = 30 * time.Second
	deliveryPollInterval  = time.Second
	deliveryBatchSize     = 10
)

type vapidPushSender struct {
	config Config
	client *http.Client
}

func newVAPIDPushSender(config Config) pushSender {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, errors.New("push endpoint DNS resolution failed")
			}
			dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
			var lastErr error
			for _, entry := range ips {
				if !isPublicPushIP(entry.IP) {
					continue
				}
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(entry.IP.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			if lastErr != nil {
				return nil, errors.New("push endpoint connection failed")
			}
			return nil, errors.New("push endpoint resolved to a non-public address")
		},
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     30 * time.Second,
	}
	return &vapidPushSender{config: config, client: &http.Client{Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func isPublicPushIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
			_, network, _ := net.ParseCIDR(cidr)
			if network.Contains(v4) {
				return false
			}
		}
		return true
	}
	// Accept only globally assigned unicast IPv6 space and exclude documentation,
	// Teredo, and 6to4 transition ranges.
	_, global, _ := net.ParseCIDR("2000::/3")
	if !global.Contains(ip) {
		return false
	}
	for _, cidr := range []string{"2001:db8::/32", "2001::/32", "2002::/16"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func (s *vapidPushSender) Send(ctx context.Context, subscription webpush.Subscription, payload []byte, topic string) (*http.Response, error) {
	if err := validatePushEndpoint(subscription.Endpoint); err != nil {
		return nil, errors.New("stored push endpoint is invalid")
	}
	parsed, err := url.Parse(subscription.Endpoint)
	if err != nil || parsed.Scheme != "https" {
		return nil, errors.New("stored push endpoint is invalid")
	}
	if net.ParseIP(parsed.Hostname()) != nil && !isPublicPushIP(net.ParseIP(parsed.Hostname())) {
		return nil, errors.New("push endpoint is not publicly routable")
	}
	return webpush.SendNotificationWithContext(ctx, payload, &subscription, &webpush.Options{
		HTTPClient: s.client, Subscriber: s.config.VAPIDSubject,
		VAPIDPublicKey: s.config.VAPIDPublicKey, VAPIDPrivateKey: s.config.VAPIDPrivateKey,
		TTL: 3600, Topic: topic, Urgency: webpush.UrgencyHigh,
	})
}

func (a *App) deliveryLoop(ctx context.Context) {
	ticker := time.NewTicker(deliveryPollInterval)
	defer ticker.Stop()
	for {
		if err := a.expirePendingDeliveries(ctx); err != nil {
			slog.Error("push_delivery_expiration_failed", "error", err.Error())
		}
		processed := false
		for count := 0; count < deliveryBatchSize; count++ {
			if ctx.Err() != nil {
				return
			}
			delivery, err := a.claimDelivery(ctx)
			if err != nil {
				slog.Error("push_delivery_claim_failed", "error", err.Error())
				break
			}
			if delivery == nil {
				break
			}
			processed = true
			if err := a.deliverClaimed(ctx, *delivery); err != nil {
				slog.Error("push_delivery_update_failed", "event_id", delivery.EventID, "delivery_id", delivery.ID, "error", err.Error())
			}
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) claimDelivery(ctx context.Context) (*Delivery, error) {
	var item Delivery
	found := false
	now := time.Now().UTC()
	leaseID, leaseUntil := uuid.New(), now.Add(deliveryLeaseDuration)
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Table(Delivery{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_attempt_at <= ? AND expires_at > ? AND (lease_until IS NULL OR lease_until <= ?)", "pending", now, now, now).
			Order("next_attempt_at ASC, created_at ASC").Limit(1).Take(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		result := tx.Table(Delivery{}.TableName()).Where("id = ? AND status = ?", item.ID, "pending").Updates(map[string]any{
			"lease_id": leaseID, "lease_until": leaseUntil, "attempts": gorm.Expr("attempts + 1"), "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("push delivery lease was not acquired")
		}
		item.LeaseID, item.LeaseUntil = &leaseID, &leaseUntil
		item.Attempts++
		return nil
	})
	if err != nil || !found {
		return nil, err
	}
	return &item, nil
}

func (a *App) deliverClaimed(ctx context.Context, item Delivery) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Resolve the subscription without locking the delivery first. All subscription
		// revocation paths lock subscription rows before delivery rows, so this lock
		// order avoids a revoke-versus-send deadlock.
		var candidate Delivery
		if err := tx.Table(Delivery{}.TableName()).
			Where("id = ? AND lease_id = ? AND status = ?", item.ID, item.LeaseID, "pending").Take(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var subscription Subscription
		if err := tx.Table(Subscription{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", candidate.SubscriptionID).Take(&subscription).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return a.finishMissingSubscription(ctx, tx, item)
			}
			return err
		}
		var delivery Delivery
		if err := tx.Table(Delivery{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND lease_id = ? AND status = ?", item.ID, item.LeaseID, "pending").Take(&delivery).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := time.Now().UTC()
		if subscription.DisabledAt != nil || subscription.AccountID != delivery.AccountID {
			return a.finishDelivery(tx, delivery, "cancelled", "subscription_inactive", "")
		}
		if !delivery.ExpiresAt.After(now) {
			return a.finishDelivery(tx, delivery, "expired", "event_too_old", "")
		}
		var notification notificationPayload
		if err := jsonUnmarshalStrict(delivery.Payload, &notification); err != nil || notification.EventID != delivery.EventID || notification.DetailPath != "/events/"+delivery.EventID.String() {
			return a.finishDelivery(tx, delivery, "permanent_failure", "invalid_payload", "")
		}
		p256dh, err := a.box.open(subscription.P256dhEncrypted)
		if err != nil {
			return a.finishDelivery(tx, delivery, "permanent_failure", "encrypted_key_invalid", "")
		}
		auth, err := a.box.open(subscription.AuthEncrypted)
		if err != nil {
			return a.finishDelivery(tx, delivery, "permanent_failure", "encrypted_key_invalid", "")
		}
		webSubscription := webpush.Subscription{Endpoint: subscription.Endpoint, Keys: webpush.Keys{
			P256dh: base64.RawURLEncoding.EncodeToString(p256dh), Auth: base64.RawURLEncoding.EncodeToString(auth),
		}}
		pushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		response, sendErr := a.sender.Send(pushCtx, webSubscription, delivery.Payload, delivery.EventID.String())
		cancel()
		if sendErr != nil {
			return a.retryDelivery(tx, delivery, "network_error", "")
		}
		if response == nil {
			return a.retryDelivery(tx, delivery, "invalid_push_response", "")
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		switch {
		case response.StatusCode >= 200 && response.StatusCode < 300:
			return a.finishDelivery(tx, delivery, "accepted", "accepted", "")
		case response.StatusCode == http.StatusGone || response.StatusCode == http.StatusNotFound:
			if err := tx.Table(Subscription{}.TableName()).Where("id = ? AND disabled_at IS NULL", subscription.ID).
				Updates(map[string]any{"disabled_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Table(Delivery{}.TableName()).Where("subscription_id = ? AND id <> ? AND status = ?", subscription.ID, delivery.ID, "pending").
				Updates(map[string]any{"status": "cancelled", "last_class": "subscription_gone", "lease_id": nil, "lease_until": nil, "updated_at": now}).Error; err != nil {
				return err
			}
			return a.finishDelivery(tx, delivery, "permanent_failure", "subscription_gone", "")
		case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
			return a.retryDelivery(tx, delivery, "temporary_http_error", fmt.Sprintf("http_%d", response.StatusCode))
		default:
			return a.finishDelivery(tx, delivery, "permanent_failure", "permanent_http_error", fmt.Sprintf("http_%d", response.StatusCode))
		}
	})
}

func (a *App) finishMissingSubscription(ctx context.Context, tx *gorm.DB, item Delivery) error {
	var delivery Delivery
	if err := tx.WithContext(ctx).Table(Delivery{}.TableName()).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND lease_id = ? AND status = ?", item.ID, item.LeaseID, "pending").Take(&delivery).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return a.finishDelivery(tx, delivery, "cancelled", "subscription_missing", "")
}

func (a *App) finishDelivery(tx *gorm.DB, delivery Delivery, status, category, detail string) error {
	now := time.Now().UTC()
	updates := map[string]any{"status": status, "last_class": category, "lease_id": nil, "lease_until": nil, "updated_at": now}
	if status == "accepted" {
		updates["accepted_at"] = now
	}
	if detail != "" {
		updates["last_error"] = detail
	} else {
		updates["last_error"] = ""
	}
	return tx.Table(Delivery{}.TableName()).Where("id = ? AND lease_id = ?", delivery.ID, delivery.LeaseID).Updates(updates).Error
}

func (a *App) retryDelivery(tx *gorm.DB, delivery Delivery, category, detail string) error {
	attempt := delivery.Attempts
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	now := time.Now().UTC()
	return tx.Table(Delivery{}.TableName()).Where("id = ? AND lease_id = ?", delivery.ID, delivery.LeaseID).Updates(map[string]any{
		"status": "pending", "next_attempt_at": now.Add(delay), "last_class": category, "last_error": detail,
		"lease_id": nil, "lease_until": nil, "updated_at": now,
	}).Error
}

func jsonUnmarshalStrict(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func (a *App) deliveryQueueSize(ctx context.Context) (int64, error) {
	var count int64
	err := a.db.WithContext(ctx).Table(Delivery{}.TableName()).Where("status = ?", "pending").Count(&count).Error
	return count, err
}

func (a *App) expirePendingDeliveries(ctx context.Context) error {
	now := time.Now().UTC()
	return a.db.WithContext(ctx).Table(Delivery{}.TableName()).Where("status = ? AND expires_at <= ? AND (lease_until IS NULL OR lease_until <= ?)", "pending", now, now).
		Updates(map[string]any{"status": "expired", "last_class": "event_too_old", "lease_id": nil, "lease_until": nil, "updated_at": now}).Error
}
