package main

import (
	"os"

	"github.com/samber/lo"
	"github.com/wrouesnel/p2cli/pkg/entrypoint"
	"github.com/wrouesnel/p2cli/pkg/envutil"
)

func main() {
	env := lo.Must(envutil.FromEnvironment(os.Environ()))

	args := entrypoint.LaunchArgs{
		StdIn:  os.Stdin,
		StdOut: os.Stdout,
		StdErr: os.Stderr,
		Env:    env,
		Args:   os.Args[1:],
	}
	ret := entrypoint.Entrypoint(args)
	os.Exit(ret)
}
