package main

import (
	"errors"
	"testing"
)

// TestResolveCloneTarget verifies vat's policy that a clone must land somewhere
// other than its own source: an unset --target-db defaults to --db, and a clone
// that ends up in the same database under the same assessment name is rejected
// before any VECTR call is made.
func TestResolveCloneTarget(t *testing.T) {
	cases := map[string]struct {
		sourceDB, targetDB       string
		assessment, targetAssess string
		campaignOnly             bool
		wantDB                   string
		wantErr                  error
	}{
		"same db, new name":             {"env1", "", "A", "A copy", false, "env1", nil},
		"explicit same db, new name":    {"env1", "env1", "A", "A copy", false, "env1", nil},
		"other db, same name":           {"env1", "env2", "A", "A", false, "env2", nil},
		"other db, new name":            {"env1", "env2", "A", "A copy", false, "env2", nil},
		"same db, same name":            {"env1", "", "A", "A", false, "", ErrCloneOntoItself},
		"explicit same db, same name":   {"env1", "env1", "A", "A", false, "", ErrCloneOntoItself},
		"same db, name differs by ws":   {"env1", "  ", "A", " A ", false, "", ErrCloneOntoItself},
		"same db, name differs by case": {"env1", "", "A", "a", false, "env1", nil},
		// Campaign-only mode targets an *existing* assessment, so copying a campaign
		// back into the assessment it came from is legitimate, not a self-clone.
		"campaign only, same db and name": {"env1", "", "A", "A", true, "env1", nil},
		"campaign only, other db":         {"env1", "env2", "A", "A", true, "env2", nil},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotDB, err := resolveCloneTarget(tc.sourceDB, tc.targetDB, tc.assessment, tc.targetAssess, tc.campaignOnly)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				if gotDB != tc.wantDB {
					t.Errorf("expected target db %q, got: %q", tc.wantDB, gotDB)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got: %v", tc.wantErr, err)
			}
		})
	}
}

// TestCloneFlagRegistration guards the flag surface of the clone command: the
// aliases and the target-db default must exist, and clone must never expose a
// way to preserve the source globalId - a clone is by definition a new object.
func TestCloneFlagRegistration(t *testing.T) {
	t.Run("no reset-id escape hatch", func(t *testing.T) {
		for _, name := range []string{"reset-id", "keep-id", "no-reset-id"} {
			if cloneCmd.Flags().Lookup(name) != nil {
				t.Errorf("clone must not register a --%s flag", name)
			}
		}
	})

	t.Run("target-db defaults to empty", func(t *testing.T) {
		flag := cloneCmd.Flags().Lookup("target-db")
		if flag == nil {
			t.Fatal("expected a --target-db flag")
		}
		if flag.DefValue != "" {
			t.Errorf("expected --target-db to default to empty, got: %q", flag.DefValue)
		}
	})

	t.Run("db and env aliases registered", func(t *testing.T) {
		for _, name := range []string{"db", "env", "target-env"} {
			if cloneCmd.Flags().Lookup(name) == nil {
				t.Errorf("expected a --%s flag", name)
			}
		}
	})

	t.Run("dedicated vars, not shared with transfer", func(t *testing.T) {
		// transfer.go's init() runs after cloner.go's; if clone bound its flags to
		// the shared vars, transfer's defaults would clobber clone's. Assert the
		// actual binding by writing through the flag and checking which variable
		// moved - comparing the flag value to the clone var would just compare two
		// empty strings and pass even when the binding is wrong.
		bindings := []struct {
			flag       string
			cloneVar   *string
			sharedVar  *string
			sharedName string
		}{
			{"assessment-name", &cloneAssessmentName, &assessmentName, "assessmentName"},
			{"target-assessment-name", &cloneTargetAssessmentName, &targetAssessmentName, "targetAssessmentName"},
			{"db", &cloneSourceDB, &sourceDB, "sourceDB"},
			{"target-db", &cloneTargetDB, &targetDB, "targetDB"},
			{"hostname", &cloneHostname, &sourceHostname, "sourceHostname"},
		}

		for _, b := range bindings {
			origClone, origShared := *b.cloneVar, *b.sharedVar
			t.Cleanup(func() { *b.cloneVar, *b.sharedVar = origClone, origShared })

			const sentinel = "vat-clone-binding-sentinel"
			if err := cloneCmd.Flags().Set(b.flag, sentinel); err != nil {
				t.Fatalf("could not set --%s: %v", b.flag, err)
			}
			if *b.cloneVar != sentinel {
				t.Errorf("--%s is not bound to clone's own variable", b.flag)
			}
			if *b.sharedVar == sentinel {
				t.Errorf("--%s is bound to the shared %s variable", b.flag, b.sharedName)
			}
		}
	})
}
