# Jobs dashboard reference

`15-jobs` is a deterministic jobs/log dashboard that demonstrates the stage-6
application pattern without pretending to be a queue client:

- three selectable jobs with queued, running, succeeded, failed, and canceled states;
- progress updates through the existing `lux.Command`/`event.Message` seam;
- bounded per-job log history (`80` lines) with dropped-line telemetry;
- follow-tail that yields when the log is manually scrolled (`l`/`L`) and resumes with `f`;
- cancellation (`x`) and retry (`r`) with a per-job context;
- stale-result protection by monotonic request id and active job identity.

The demo source emits five deterministic steps. Replace `jobSource` with a real
queue or worker adapter in an application; the example deliberately does not
claim transport, persistence, or host integration.

Run it with:

```sh
go run ./examples/15-jobs
```

Controls: `Space` run the selected job, `r` retry, `x` cancel, `f` follow the
latest log line, `l`/`L` pause and move through the log, arrows select a job,
and `Esc` quits.
