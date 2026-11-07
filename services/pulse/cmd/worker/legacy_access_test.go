package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
)

type logReaderAccessChecker interface {
	CheckReadOnly(context.Context) (newapi.AccessCheckReport, error)
}

func verifyLogReaderAccess(ctx context.Context, reader logReaderAccessChecker) (newapi.AccessCheckReport, error) {
	if reader == nil {
		return newapi.AccessCheckReport{}, errors.New("new-api log reader is nil")
	}
	report, err := reader.CheckReadOnly(ctx)
	if err != nil {
		return report, fmt.Errorf("check new-api log reader access: %w", err)
	}
	if !report.Readable || !report.ReadOnly {
		return report, errors.New("new-api log reader is not proven read-only")
	}
	return report, nil
}
