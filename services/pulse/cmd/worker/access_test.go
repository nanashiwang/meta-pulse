package main

import (
	"context"
	"errors"
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
)

type fakeLogReaderAccessChecker struct {
	report newapi.AccessCheckReport
	err    error
}

func (f fakeLogReaderAccessChecker) CheckReadOnly(context.Context) (newapi.AccessCheckReport, error) {
	return f.report, f.err
}

func TestVerifyLogReaderAccess(t *testing.T) {
	for _, test := range []struct {
		name    string
		reader  logReaderAccessChecker
		wantErr bool
	}{
		{name: "proven read only", reader: fakeLogReaderAccessChecker{report: newapi.AccessCheckReport{Readable: true, ReadOnly: true}}, wantErr: false},
		{name: "not readable", reader: fakeLogReaderAccessChecker{report: newapi.AccessCheckReport{ReadOnly: true}}, wantErr: true},
		{name: "write capable", reader: fakeLogReaderAccessChecker{report: newapi.AccessCheckReport{Readable: true}}, wantErr: true},
		{name: "check failure", reader: fakeLogReaderAccessChecker{err: errors.New("database unavailable")}, wantErr: true},
		{name: "nil reader", reader: nil, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := verifyLogReaderAccess(context.Background(), test.reader)
			if (err != nil) != test.wantErr {
				t.Fatalf("verifyLogReaderAccess() error=%v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}
