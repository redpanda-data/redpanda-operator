# Alias

Alias is a small program to effectively emulate the following shell script:

```sh
#!/bin/sh
new-command subcommand "$@"
```

in environments that do not have `sh` installed, such as
[distroless](https://github.com/GoogleContainerTools/distroless).

It and it's usages may be removed once all deployments of the operator have
been updated to use/reference the `/redpanda-operator` binary.

## Building

Alias must have `AliasTo` set to a space separated value using the `-X`
[`-ldflags` option](https://pkg.go.dev/cmd/link).

```sh
go build -ldflags -X main.AliasTo=cat -o ./dog ./main.go
```
