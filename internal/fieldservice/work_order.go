package fieldservice

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const FeatureKey = "field-service-follow-up"

type FlagReader interface {
	FlagEnabled(context.Context, string) (bool, error)
}

type WorkOrder struct {
	ID           string   `json:"id"`
	Dispatch     string   `json:"dispatch_status"`
	PhotoRefs    []string `json:"photo_refs"`
	FollowUpDue  string   `json:"technician_follow_up_due,omitempty"`
	FeatureTrack string   `json:"feature_track"`
}

type Intake struct {
	WorkOrderID string   `json:"work_order_id"`
	PhotoRefs   []string `json:"photo_refs"`
}

type Processor struct {
	flags FlagReader
	now   func() time.Time
}

func NewProcessor(flags FlagReader, now func() time.Time) *Processor {
	if now == nil {
		now = time.Now
	}
	return &Processor{flags: flags, now: now}
}

func (p *Processor) Process(ctx context.Context, in Intake) (WorkOrder, error) {
	if in.WorkOrderID == "" {
		return WorkOrder{}, errors.New("work_order_id is required")
	}
	enabled, err := p.flags.FlagEnabled(ctx, FeatureKey)
	if err != nil {
		return WorkOrder{}, fmt.Errorf("read field-service flag: %w", err)
	}
	order := WorkOrder{ID: in.WorkOrderID, Dispatch: "queued", FeatureTrack: "baseline"}
	if enabled {
		order.Dispatch = "technician_dispatched"
		order.PhotoRefs = append([]string(nil), in.PhotoRefs...)
		order.FollowUpDue = p.now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
		order.FeatureTrack = "field-service"
	}
	return order, nil
}
