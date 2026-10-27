package cascadingconfig

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// envNameError reports an environment name that gives an invalid file name, once per name.
func (ld *loading) envNameError(env string, err error) {
	op := ld.envSource
	if op == "" {
		op = "load"
	}
	for _, e := range ld.entries {
		var oe *OptionError
		if errors.As(e.err, &oe) && oe.Op == op && strings.Contains(oe.Error(), fmt.Sprintf("%q", env)) {
			return
		}
	}
	ld.addError(&OptionError{BaseError: BaseError{Op: op, Err: fmt.Errorf("the environment %q: %w", env, err)}})
}

// selectEnvs works out the environments: platform ones first, then the selected ones, from WithEnvs or {NAME}_ENV.
func (ld *loading) selectEnvs() error {
	c := ld.c
	selected := c.envs
	ld.envSource = "WithEnvs"
	if selected == nil {
		ld.envSource = c.vars[BindEnv]
		for _, e := range strings.Split(ld.binding(BindEnv), ",") {
			if e = strings.TrimSpace(e); e != "" {
				if err := checkName(e); err != nil {
					return &OptionError{BaseError: BaseError{Op: ld.envSource, Err: fmt.Errorf("invalid environment name %q: %w", e, err)}}
				}
				selected = append(selected, e)
			}
		}
	}
	if selected == nil && ld.settings.env != nil {
		selected, ld.envSource = ld.settings.env, ld.settings.envFrom
	}
	var envs []string
	for i, e := range selected {
		if !slices.Contains(selected[i+1:], e) {
			envs = append(envs, e)
		}
	}
	for _, p := range platformEnvs(c.host.GOOS(), c.host.GOARCH()) {
		if !slices.Contains(envs, p) {
			ld.envs = append(ld.envs, p)
			ld.platformEnvs = append(ld.platformEnvs, p)
		}
	}
	ld.envs = append(ld.envs, envs...)
	ld.selectedEnvs = envs
	return nil
}
