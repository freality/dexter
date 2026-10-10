package docker

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// labelResource marks containers dex created, valued with the resource name.
const labelResource = "dev.dex.resource"

var restartModes = []string{"no", "always", "on-failure", "unless-stopped"}

// spec is a container resource's properties, decoded and validated.
type spec struct {
	Image   string
	Ports   []string
	Env     map[string]string
	Command []string
	Restart string

	bindings network.PortMap
}

// sameExceptRestart reports whether s and o differ in Restart at most, so
// the change can be applied in place without recreating the container.
func (s spec) sameExceptRestart(o spec) bool {
	return s.Image == o.Image && slices.Equal(s.Ports, o.Ports) &&
		maps.Equal(s.Env, o.Env) && slices.Equal(s.Command, o.Command)
}

func parseSpec(props map[string]any) (spec, error) {
	s := spec{Restart: "unless-stopped", bindings: network.PortMap{}}
	for _, key := range slices.Sorted(maps.Keys(props)) {
		var err error
		v := props[key]
		switch key {
		case "image":
			s.Image, _ = v.(string)
			if s.Image == "" {
				err = fmt.Errorf("required string")
			}
		case "ports":
			if s.Ports, err = stringList(v); err == nil {
				for _, raw := range s.Ports {
					if err = s.bind(raw); err != nil {
						break
					}
				}
			}
		case "env":
			s.Env, err = stringMap(v)
		case "command":
			s.Command, err = stringList(v)
		case "restart":
			s.Restart, _ = v.(string)
			if !slices.Contains(restartModes, s.Restart) {
				err = fmt.Errorf("must be one of %s", strings.Join(restartModes, ", "))
			}
		default:
			err = fmt.Errorf("unknown (supported: command, env, image, ports, restart)")
		}
		if err != nil {
			return spec{}, fmt.Errorf("property %q: %w", key, err)
		}
	}
	if s.Image == "" {
		return spec{}, fmt.Errorf(`property "image": required string`)
	}
	return s, nil
}

// bind parses one "host:container[/proto]" mapping into s.bindings.
func (s *spec) bind(raw string) error {
	host, ctr, ok := strings.Cut(raw, ":")
	if !ok {
		return fmt.Errorf("%q must be \"host:container[/proto]\"", raw)
	}
	if n, err := strconv.ParseUint(host, 10, 16); err != nil || n == 0 {
		return fmt.Errorf("%q has invalid host port %q", raw, host)
	}
	port, err := network.ParsePort(ctr)
	if err != nil {
		return fmt.Errorf("%q has invalid container port %q", raw, ctr)
	}
	s.bindings[port] = append(s.bindings[port], network.PortBinding{HostPort: host})
	return nil
}

func stringList(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a list of strings")
	}
	out := make([]string, len(items))
	for i, item := range items {
		if out[i], ok = item.(string); !ok {
			return nil, fmt.Errorf("must be a list of strings")
		}
	}
	return out, nil
}

func stringMap(v any) (map[string]string, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object of strings")
	}
	out := make(map[string]string, len(obj))
	for _, k := range slices.Sorted(maps.Keys(obj)) {
		if out[k], ok = obj[k].(string); !ok {
			return nil, fmt.Errorf("value of %q must be a string", k)
		}
	}
	return out, nil
}

// createOptions renders s as the Docker create request for container name.
func (s spec) createOptions(name string) client.ContainerCreateOptions {
	env := make([]string, 0, len(s.Env))
	for _, k := range slices.Sorted(maps.Keys(s.Env)) {
		env = append(env, k+"="+s.Env[k])
	}
	exposed := network.PortSet{}
	for p := range s.bindings {
		exposed[p] = struct{}{}
	}
	return client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:        s.Image,
			Env:          env,
			Cmd:          s.Command,
			ExposedPorts: exposed,
			Labels:       map[string]string{labelResource: name},
		},
		HostConfig: &container.HostConfig{
			PortBindings:  s.bindings,
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(s.Restart)},
		},
	}
}
