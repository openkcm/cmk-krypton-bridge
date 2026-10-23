package eventprocessor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openkcm/orbital"
	"github.com/openkcm/orbital/codec"

	goAmqp "github.com/Azure/go-amqp"
	orbitalamqp "github.com/openkcm/orbital/client/amqp"

	"github.com/openkcm/cmk-krypton-bridge/pkg/config"
)

var (
	ErrTargetNotConfigured = errors.New("target not configured for region")
)

// KryptonReconciler mirrors CMK's CryptoReconciler but is the Operator side:
// it receives orbital Tasks from CMK and (in production) calls Krypton.
// Naming follows CMK conventions as closely as possible.
type KryptonReconciler struct {
	lg            *slog.Logger
	jobHandlerMap map[JobType]orbital.HandlerFunc
}

// NewKryptonReconciler creates a KryptonReconciler and wires up all job handlers.
// Mirrors CMK's NewCryptoReconciler structure.
func NewKryptonReconciler(lg *slog.Logger) *KryptonReconciler {
	r := &KryptonReconciler{lg: lg}

	keyHandler := NewKeyJobHandler(lg)
	keyDetachHandler := NewKeyDetachJobHandler(lg)

	r.jobHandlerMap = map[JobType]orbital.HandlerFunc{
		JobTypeSystemLink:      NewSystemLinkJobHandler(lg).Handle,
		JobTypeSystemUnlink:    NewSystemUnlinkJobHandler(lg).Handle,
		JobTypeSystemSwitch:    NewSystemSwitchJobHandler(lg).Handle,
		JobTypeSystemKeyRotate: NewSystemKeyRotateJobHandler(lg).Handle,
		JobTypeKeyEnable:       keyHandler.Handle,
		JobTypeKeyDisable:      keyHandler.Handle,
		JobTypeKeyDelete:       keyHandler.Handle,
		JobTypeKeyDetach:       keyDetachHandler.Handle,
	}

	return r
}

// RegisterHandlers registers all task type handlers with the orbital.Operator.
// Mirrors CMK's TenantOperator.registerHandlers.
func (r *KryptonReconciler) RegisterHandlers(op *orbital.Operator) error {
	for jobType, handler := range r.jobHandlerMap {
		if err := op.RegisterHandler(jobType.String(), handler); err != nil {
			return fmt.Errorf("register handler %s: %w", jobType, err)
		}
	}
	return nil
}

// Start creates the AMQP client, operator, registers all handlers, and blocks until ctx is cancelled.
// Mirrors CMK's TenantOperator.RunOperator.
func (r *KryptonReconciler) Start(ctx context.Context, cfg config.OrbitalConfig) error {
	amqpClient, err := newAMQPClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create AMQP client: %w", err)
	}
	defer amqpClient.Close(ctx)

	op, err := orbital.NewOperator(orbital.TargetOperator{Client: amqpClient})
	if err != nil {
		return fmt.Errorf("create orbital operator: %w", err)
	}

	if err := r.RegisterHandlers(op); err != nil {
		return err
	}

	r.lg.InfoContext(ctx, "orbital: KryptonReconciler running",
		slog.String("url", cfg.AMQP.URL),
		slog.String("source", cfg.AMQP.Source),
		slog.String("target", cfg.AMQP.Target))

	go func() {
		if err := op.ListenAndRespond(ctx); err != nil && ctx.Err() == nil {
			r.lg.ErrorContext(ctx, "orbital: ListenAndRespond failed", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	r.lg.InfoContext(ctx, "orbital: KryptonReconciler stopped")
	return nil
}

// newAMQPClient mirrors CMK's createAMQPTargets / getAMQPOptions — builds the AMQP client
// from the Bridge config.
func newAMQPClient(ctx context.Context, cfg config.OrbitalConfig) (*orbitalamqp.Client, error) {
	opts, err := getAMQPOptions(cfg)
	if err != nil {
		return nil, err
	}

	return orbitalamqp.NewClient(
		ctx,
		codec.Proto{},
		orbitalamqp.ConnectionInfo{
			URL:    cfg.AMQP.URL,
			Target: cfg.AMQP.Target,
			Source: cfg.AMQP.Source,
		},
		opts...,
	)
}

// getAMQPOptions mirrors CMK's getAMQPOptions.
func getAMQPOptions(cfg config.OrbitalConfig) ([]orbitalamqp.ClientOption, error) {
	switch cfg.AMQP.Auth.Type {
	case "mtls":
		m := cfg.AMQP.Auth.MTLS
		return []orbitalamqp.ClientOption{
			orbitalamqp.WithExternalMTLS(m.CertFile, m.KeyFile, m.CAFile, m.ServerName),
		}, nil
	case "basic":
		b := cfg.AMQP.Auth.Basic
		return []orbitalamqp.ClientOption{
			orbitalamqp.WithBasicAuth(b.Username, b.Password),
		}, nil
	default:
		return []orbitalamqp.ClientOption{orbitalamqp.WithNoAuth()}, nil
	}
}

// WithMTLS mirrors CMK's operator.WithMTLS — builds a low-level goAmqp ClientOption for mTLS.
// Used when the TLS config is loaded externally (e.g. from commoncfg).
func WithMTLS(certFile, keyFile, caFile, serverName string) orbitalamqp.ClientOption {
	return func(o *goAmqp.ConnOptions) error {
		return orbitalamqp.WithExternalMTLS(certFile, keyFile, caFile, serverName)(o)
	}
}
