package cli

import (
	"flag"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func dispatchInitializationFlags(fs *flag.FlagSet) func() (*runtime.DispatchBackpressureConfig, error) {
	maximum := fs.Int("dispatch-max-unreceived-results", -1, "freeze explicit S6 Builder reception backpressure on a new Runtime (>=1)")
	resume := fs.Int("dispatch-resume-unreceived-results", 0, "resume Builder registration when original unreceived count reaches this threshold (0 <= resume < max)")
	return func() (*runtime.DispatchBackpressureConfig, error) {
		hasMax, hasResume := false, false
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "dispatch-max-unreceived-results":
				hasMax = true
			case "dispatch-resume-unreceived-results":
				hasResume = true
			}
		})
		if !hasMax {
			if hasResume {
				return nil, fmt.Errorf("dispatch resume threshold requires an explicit max threshold")
			}
			return nil, nil
		}
		config := &runtime.DispatchBackpressureConfig{MaxUnreceivedResults: *maximum, ResumeUnreceivedResults: *resume}
		return config, config.Validate()
	}
}
