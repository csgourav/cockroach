// Copyright 2024 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package mixedversion

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/option"
	"github.com/cockroachdb/cockroach/pkg/cmd/roachtest/roachtestutil/clusterupgrade"
	"github.com/cockroachdb/cockroach/pkg/util/randutil"
	"github.com/stretchr/testify/require"
)

// TestPreserveDowngradeOptionRandomizerMutator tests basic behaviour
// of the mutator by directly inspecting the mutations it produces
// when `Generate` is called. This mutator is also tested as part of
// the planner test suite, with a datadriven test.
func TestPreserveDowngradeOptionRandomizerMutator(t *testing.T) {
	numUpgrades := 3
	mvt := newBasicUpgradeTest(NumUpgrades(numUpgrades))
	plan, err := mvt.plan()
	require.NoError(t, err)

	var mut preserveDowngradeOptionRandomizerMutator
	mutations := mut.Generate(newRand(), plan)
	require.NotEmpty(t, mutations)
	require.True(t, len(mutations)%2 == 0, "should produce even number of mutations") // one removal and one insertion per upgrade

	// First half of mutations should be the removals of the existing
	// `allowUpgradeStep`s.
	for j := 0; j < numUpgrades/2; j++ {
		require.Equal(t, mutationRemove, mutations[j].op)
		require.IsType(t, allowUpgradeStep{}, mutations[j].reference.impl)
	}

	// Second half of mutations should be insertions of new
	// `allowUpgradeStep`s.
	for j := numUpgrades / 2; j < len(mutations); j++ {
		require.Equal(t, mutationInsertBefore, mutations[j].op)
		require.IsType(t, allowUpgradeStep{}, mutations[j].impl)
	}
}

// TestClusterSettingMutator does not validate the specific mutations
// generated by the clusterSettingMutartor; instead, it validates the
// invariants that the mutator should provide. For example: expected
// number of mutations, no consecutive RESETs, no consecutive SETs to
// same value, etc. This is done for different configurations of the
// mutator.
func TestClusterSettingMutator(t *testing.T) {
	currentVersion := "v24.2.12"
	defer withTestBuildVersion(currentVersion)()

	// verifyVersionRequirement ensures that the provided mutation `m`
	// is valid with respect to the requested `minVersion`. In other
	// words, there should be at least one node running at least
	// `minVersion`, so that we are able to service the cluster setting
	// change request.
	verifyVersionRequirement := func(minVersion *clusterupgrade.Version, m mutation) {
		if minVersion == nil {
			return
		}

		var nodesInValidVersion option.NodeListOption
		stepContext := m.reference.context
		for _, node := range stepContext.System.Descriptor.Nodes {
			nodeV, err := stepContext.NodeVersion(node)
			require.NoError(t, err)
			if nodeV.AtLeast(minVersion) {
				nodesInValidVersion = append(nodesInValidVersion, node)
			}
		}

		require.NotEmpty(
			t, nodesInValidVersion,
			"attempting to change setting but no node can service request",
		)
	}

	verifyMutations := func(
		numUpgrades int, possibleValues []interface{}, options []clusterSettingMutatorOption,
	) bool {
		mvt := newBasicUpgradeTest(NumUpgrades(numUpgrades))
		mvt.predecessorFunc = func(rng *rand.Rand, v *clusterupgrade.Version, n int) ([]*clusterupgrade.Version, error) {
			// NB: we need at least `maxUpgrades` (defined in the generator
			// below) versions here.
			return parseVersions([]string{"v19.2.0", "v22.1.28", "v22.2.2", "v23.1.9", "v23.2.7", "v24.1.3"}), nil
		}

		plan, err := mvt.plan()
		require.NoError(t, err)

		const settingName = "test_cluster_setting"
		mut := newClusterSettingMutator(settingName, possibleValues, options...)
		mutations := mut.Generate(newRand(), plan)

		// Number of mutations should be 1 <= n <= maxChanges
		require.GreaterOrEqual(t, len(mutations), 1, "plan:\n%s", plan.PrettyPrint())
		require.LessOrEqual(t, len(mutations), mut.maxChanges)

		// For every mutation:
		var prevImpl singleStepProtocol
		for j, m := range mutations {
			verifyVersionRequirement(mut.minVersion, m)

			switch step := m.impl.(type) {
			case setClusterSettingStep:
				require.Equal(t, mut.minVersion, step.minVersion)
				require.Equal(t, mut.name, step.name)
				require.Contains(t, mut.possibleValues, step.value)

				// If this is the first mutation being generated, there's
				// nothing to verify: a SET mutation is always valid as the
				// first step.
				if j == 0 {
					break
				}

				// If we are attempting to SET the cluster setting to some
				// specific value:
				switch prevStep := prevImpl.(type) {
				case setClusterSettingStep:
					// And we had previously set the cluster setting to some
					// other value, we verify that the new value is different
					// from the previous one.
					require.NotEqualValues(
						t, step.value, prevStep.value,
						"found two consecutive SET steps to value %v", step.value,
					)

				case resetClusterSettingStep:
					// If the cluster setting was previously RESET, there's
					// nothing to validate: it is valid to set the cluster
					// setting to any value.

				default:
					t.Fatalf("unexpected previous mutation type: %T", prevStep)
				}

			case resetClusterSettingStep:
				require.Equal(t, mut.minVersion, step.minVersion)
				require.Equal(t, mut.name, step.name)

				// If we are attempting to RESET the cluster setting:

				// We verify that this step is not the first mutation: we
				// should always be setting the cluster setting to some value
				// first.
				require.Greater(t, j, 0, "first step cannot RESET cluster setting")

				// We also verify that the previous step SET the cluster
				// setting to some value; we should not be attempting to RESET
				// a cluster setting twice.
				require.IsType(
					t, setClusterSettingStep{}, prevImpl,
					"step prior to RESET should be SET, found %T", prevImpl,
				)

			default:
				t.Fatalf("unexpected mutation type: %T", step)
			}

			prevImpl = m.impl
		}

		return true
	}

	rng, seed := randutil.NewPseudoRand()
	t.Logf("using random seed %d", seed)

	const maxPossibleValues = 10

	// generator randomizes the input to `verifyMutator`. Returns a
	// random number of upgrades to be used when generating the test
	// plan, the list of possible values for the cluster setting, and
	// options to be passed to the mutator, if any.
	generator := func(values []reflect.Value, rng *rand.Rand) {
		minUpgrades, maxUpgrades := 3, 6
		numUpgrades := rng.Intn(maxUpgrades-minUpgrades+1) + minUpgrades

		// Choose the type of value for our cluster setting.
		possibleValuesType := []string{"bools", "ints", "strings"}
		var possibleValues []interface{}
		switch typ := possibleValuesType[rng.Intn(len(possibleValuesType))]; typ {
		case "bools":
			// Exercise the case where we are only possibly setting a
			// boolean cluster setting to one value.
			if rng.Float64() < 0.5 {
				possibleValues = []interface{}{rng.Float64() < 0.5}
			} else {
				possibleValues = []interface{}{true, false}
			}

		case "ints":
			numValues := 1 + rng.Intn(maxPossibleValues)
			for j := 0; j < numValues; j++ {
				possibleValues = append(possibleValues, rng.Int())
			}

		case "strings":
			const maxStrLen = 64
			numValues := 1 + rng.Intn(maxPossibleValues)
			for j := 0; j < numValues; j++ {
				strLen := 1 + rng.Intn(maxStrLen)
				possibleValues = append(
					possibleValues,
					randutil.RandString(rng, strLen, randutil.PrintableKeyAlphabet),
				)
			}

		default:
			t.Fatalf("unrecognized possibleValues type: %q", typ)
		}

		var options []clusterSettingMutatorOption
		if rng.Float64() < 0.5 {
			if rng.Float64() < 0.5 {
				options = append(options, clusterSettingMinimumVersion("v23.2.12"))
			} else {
				// Make sure we are able to generate changes for cluster
				// settings introduced in the latest version.
				options = append(options, clusterSettingMinimumVersion(currentVersion))
			}
		}
		if rng.Float64() < 0.5 {
			const maxMaxChanges = 20
			options = append(options, clusterSettingMaxChanges(1+rng.Intn(maxMaxChanges)))
		}

		values[0] = reflect.ValueOf(numUpgrades)
		values[1] = reflect.ValueOf(possibleValues)
		values[2] = reflect.ValueOf(options)
	}

	require.NoError(t, quick.Check(verifyMutations, &quick.Config{
		MaxCount: 100,
		Rand:     rng,
		Values:   generator,
	}))
}
