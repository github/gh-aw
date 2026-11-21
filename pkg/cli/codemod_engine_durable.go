package cli

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/codemod"
)

var engineDurableCodemodLog = logger.New("cli:codemod_engine_durable")

// getEngineDurableToRequestContinuationCodemod migrates deprecated
// engine.durable and engine.pi.durable to safe-outputs.request-continuation.
func getEngineDurableToRequestContinuationCodemod() Codemod {
	return Codemod{
		ID:           "engine-durable-to-request-continuation",
		Name:         "Migrate engine.durable to safe-outputs.request-continuation",
		Description:  "Removes deprecated 'engine.durable' and 'engine.pi.durable' and enables 'safe-outputs.request-continuation', which now controls durable continuation. Durable retention-days is dropped; checkpoints use the default retention.",
		IntroducedIn: "1.5.0",
		Apply: func(content string, _ map[string]any) (string, bool, error) {
			return codemod.Apply(content, func(f *codemod.Frontmatter) error {
				removed := false
				for _, path := range [][]string{{"engine", "durable"}, {"engine", "pi", "durable"}} {
					if _, found := f.Lookup(path...); !found {
						continue
					}
					if err := f.Remove(path...); err != nil {
						return err
					}
					removed = true
					engineDurableCodemodLog.Printf("Removed deprecated %v", path)
				}
				if !removed {
					return nil
				}
				if err := f.PruneEmpty("engine", "pi"); err != nil {
					return err
				}
				if _, exists := f.Lookup("safe-outputs", "request-continuation"); exists {
					return nil
				}
				engineDurableCodemodLog.Print("Enabled safe-outputs.request-continuation")
				return f.Set([]string{"safe-outputs", "request-continuation"}, map[string]any{})
			})
		},
	}
}
