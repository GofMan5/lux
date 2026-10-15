package main

import (
	"context"
	"errors"
	"testing"
)

type scriptedSource struct {
	steps []jobStep
	calls int
}

func (s *scriptedSource) Step(ctx context.Context, id, cursor int) jobStep {
	if err := ctx.Err(); err != nil {
		return jobStep{id: id, err: err}
	}
	if s.calls >= len(s.steps) {
		return jobStep{id: id, done: 1, total: 1, final: true}
	}
	step := s.steps[s.calls]
	s.calls++
	step.id = id
	return step
}

func TestJobsProgressTelemetryRetryAndBoundedHistory(t *testing.T) {
	source := &scriptedSource{steps: []jobStep{
		{done: 1, total: 3, line: "queued"},
		{done: 2, total: 3, line: "running"},
		{done: 3, total: 3, line: "done", final: true},
	}}
	app := newJobsApp(source)
	app.height = 20
	cmd := app.startJob(false)
	if cmd == nil {
		t.Fatal("startJob returned no command")
	}
	for cmd != nil {
		result := cmd(context.Background())
		cmd = app.applyStep(result.Data.(jobStep))
	}
	if got := app.jobs[0].status; got != succeeded {
		t.Fatalf("status=%s, want succeeded", got)
	}
	if len(app.jobs[0].logs) != 3 || app.telemetry.events != 3 || app.telemetry.completed != 1 {
		t.Fatalf("telemetry/logs wrong: %+v logs=%d", app.telemetry, len(app.jobs[0].logs))
	}

	for i := 0; i < logCap+7; i++ {
		app.appendLog(&app.jobs[0], "line")
	}
	if len(app.jobs[0].logs) != logCap || app.jobs[0].dropped != 10 || app.telemetry.dropped != 10 {
		t.Fatalf("history not bounded: len=%d dropped=%d telemetry=%d", len(app.jobs[0].logs), app.jobs[0].dropped, app.telemetry.dropped)
	}
	if !app.follow || app.logOffset != logCap-app.logRows() {
		t.Fatalf("follow tail lost: follow=%v offset=%d", app.follow, app.logOffset)
	}
	app.moveLog(-1)
	if app.follow || app.logOffset != logCap-app.logRows()-1 {
		t.Fatalf("manual log scroll wrong: follow=%v offset=%d", app.follow, app.logOffset)
	}

	cancelSource := &scriptedSource{steps: []jobStep{{done: 1, total: 3, line: "started"}}}
	cancelApp := newJobsApp(cancelSource)
	cancelCmd := cancelApp.startJob(false)
	cancelApp.cancelJob()
	result := cancelCmd(context.Background())
	if !errors.Is(result.Data.(jobStep).err, context.Canceled) {
		t.Fatalf("cancel result=%v", result.Data.(jobStep).err)
	}
	cancelApp.applyStep(result.Data.(jobStep))
	if cancelApp.jobs[0].status != canceled {
		t.Fatalf("cancel status=%s", cancelApp.jobs[0].status)
	}

	retryCmd := cancelApp.startJob(true)
	if retryCmd == nil {
		t.Fatal("retry did not start")
	}
	retryCmd = cancelApp.applyStep(jobStep{id: cancelApp.activeID, done: 1, total: 1, final: true, line: "retried"})
	if retryCmd != nil || cancelApp.jobs[0].status != succeeded {
		t.Fatalf("retry did not complete: status=%s", cancelApp.jobs[0].status)
	}
}
