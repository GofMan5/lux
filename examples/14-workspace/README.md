# Workspace example

`14-workspace` is the project/config reference flow. It is intentionally small:
filesystem access stays behind `workspaceSource`, while the model owns loading,
validation, preview, unsaved protection, and save outcomes.

Run it against a directory:

```sh
LUX_WORKSPACE_ROOT=./path/to/project go run ./examples/14-workspace
```

PowerShell:

```powershell
$env:LUX_WORKSPACE_ROOT = "C:\path\to\project"
go run ./examples/14-workspace
```

The first file is loaded through an asynchronous `Command`; a missing or
unreadable workspace stays in an error state and `Ctrl+R` retries. `config.lux`
requires `name = ...` and a positive integer `workers = ...` before `Ctrl+S`
can write. Saves stage a same-directory temporary file, sync it, and replace
the target; the source rejects path traversal. `Ctrl+P` shows an unsaved
read-only preview. `Esc` asks before discarding an unsaved draft.

Tests use the same seam with a deterministic source, covering loading,
recovery, validation, preview, unsaved protection, and replacement writes.
