module github.com/lux-tui/lux/examples/ssh

go 1.23.0

require (
	github.com/creack/pty v1.1.24
	github.com/gliderlabs/ssh v0.3.8
	github.com/lux-tui/lux v0.0.0-00010101000000-000000000000
)

require (
	github.com/anmitsu/go-shlex v0.0.0-20200514113438-38f4b401e2be // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/term v0.34.0 // indirect
)

replace github.com/lux-tui/lux => ../..
