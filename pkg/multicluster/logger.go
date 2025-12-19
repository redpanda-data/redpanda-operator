// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"go.etcd.io/raft/v3"
)

type raftLogr struct {
	logger logr.Logger
}

func (r *raftLogr) Debug(v ...any) {
	r.logger.V(1).Info("DEBUG", v...)
}

func (r *raftLogr) Debugf(format string, v ...any) {
	if format == "" {
		r.logger.V(0).Info("DEBUG", v...)
	} else {
		r.logger.V(0).Info(fmt.Sprintf("[DEBUG] %s", fmt.Sprintf(format, v...)))
	}
}

func (r *raftLogr) Error(v ...any) {
	r.logger.Error(errors.New("an error occurred"), "ERROR", v...)
}

func (r *raftLogr) Errorf(format string, v ...any) {
	if format == "" {
		r.logger.Error(errors.New("an error occurred"), "ERROR", v...)
	} else {
		text := fmt.Sprintf(format, v...)
		r.logger.Error(errors.New(text), text)
	}
}

func (r *raftLogr) Info(v ...any) {
	r.logger.V(0).Info("INFO", v...)
}

func (r *raftLogr) Infof(format string, v ...any) {
	if format == "" {
		r.logger.V(0).Info("INFO", v...)
	} else {
		r.logger.V(0).Info(fmt.Sprintf("[INFO] %s", fmt.Sprintf(format, v...)))
	}
}

func (r *raftLogr) Warning(v ...any) {
	r.logger.V(0).Info("WARN", v...)
}

func (r *raftLogr) Warningf(format string, v ...any) {
	if format == "" {
		r.logger.V(0).Info("WARN", v...)
	} else {
		r.logger.V(0).Info(fmt.Sprintf("[WARN] %s", fmt.Sprintf(format, v...)))
	}
}

func (r *raftLogr) Fatal(v ...any) {
	r.Error(v...)
	os.Exit(1)
}

func (r *raftLogr) Fatalf(format string, v ...any) {
	r.Errorf(format, v...)
	os.Exit(1)
}

func (r *raftLogr) Panic(v ...any) {
	r.Error(v...)
	panic("unexpected error")
}

func (r *raftLogr) Panicf(format string, v ...any) {
	r.Errorf(format, v...)
	panic(fmt.Sprintf(format, v...))
}

var _ raft.Logger = (*raftLogr)(nil)
