// dex-adapter-docker is the gRPC adapter binary for Docker containers.
// It wraps the in-process Docker adapter as a plugin.Adapter and serves
// it over gRPC through go-plugin.
package main

import (
	"github.com/freality/dexter/adapters/docker"
	"github.com/freality/dexter/pkg/plugin"
)

func main() {
	adapter, err := docker.New()
	if err != nil {
		panic(err)
	}
	plugin.Serve(docker.NewPlugin(adapter))
}
