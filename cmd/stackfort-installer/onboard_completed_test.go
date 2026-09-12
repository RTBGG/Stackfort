// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestOnboardPublicRoutesOnlyEmptyStateToFreshAndCompleteToLiveCheck(t *testing.T) {
	for _, scenario := range []string{"fresh", "completed", "partial", "forged", "inspection-error", "check-error", "output-error", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newOnboardControllerFixture(t, onboardAcceptAll())
			var events []string
			controller := onboardPublicController{
				inspect: func(_ context.Context, selection installapply.NativeOnboardingSource) (installapply.NativeOnboardingPrepared, bool, error) {
					events = append(events, "inspect")
					if selection != fixture.review.Source {
						t.Fatal("selection changed")
					}
					if scenario == "fresh" {
						return installapply.NativeOnboardingPrepared{}, false, nil
					}
					if scenario == "partial" || scenario == "inspection-error" {
						return installapply.NativeOnboardingPrepared{}, false, errors.New("not completed")
					}
					prepared := fixture.prepared
					if scenario == "forged" {
						prepared.RuntimePath = "/tmp/other"
					}
					return prepared, true, nil
				},
				fresh: func(context.Context, installapply.NativeOnboardingSource, io.Writer) error {
					events = append(events, "fresh")
					return nil
				},
				check: func(_ context.Context, prepared installapply.NativeOnboardingPrepared) error {
					events = append(events, "check")
					if prepared != fixture.prepared {
						t.Fatal("live check received foreign handoff")
					}
					if scenario == "check-error" {
						return errors.New("live admission failed")
					}
					return nil
				},
			}
			ctx := t.Context()
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			var stdout bytes.Buffer
			var output io.Writer = &stdout
			if scenario == "output-error" {
				output = &onboardFailOutput{failAt: 1}
			}
			err := controller.run(ctx, fixture.review.Source, output)
			switch scenario {
			case "fresh":
				if err != nil || !reflect.DeepEqual(events, []string{"inspect", "fresh"}) {
					t.Fatal(events, err)
				}
			case "completed":
				if err != nil || !reflect.DeepEqual(events, []string{"inspect", "check"}) || !strings.Contains(stdout.String(), "Live native admission checks passed") {
					t.Fatal(events, err, stdout.String())
				}
			default:
				if err == nil || strings.Contains(strings.Join(events, ","), "fresh") || strings.Contains(stdout.String(), "checks passed") {
					t.Fatal("unsafe completed rerun", events, err)
				}
				if scenario != "check-error" && strings.Contains(strings.Join(events, ","), "check") {
					t.Fatal("live mutation preceded validation", events)
				}
			}
		})
	}
}

func onboardCompletedResultFixture(t *testing.T) (installapply.Result, installapply.NativeReleaseManifest) {
	t.Helper()
	fixture := newOnboardControllerFixture(t, onboardAcceptAll())
	manifest := fixture.prepared.Manifest
	result := installapply.Result{Version: manifest.Release.Policy.Version, SourceDigest: manifest.Release.Source.SourceDigest, Status: installapply.InstallComplete, AlreadyInstalled: true}
	for _, id := range []installapply.StageID{installapply.StagePackages, installapply.StageWAFPackage, installapply.StageVinylPackage, installapply.StageIdentity, installapply.StagePayload, installapply.StageConfiguration, installapply.StageSecurity, installapply.StageNGINX, installapply.StageServices} {
		result.Stages = append(result.Stages, installapply.StageState{ID: id, Status: installapply.StageComplete, Attempts: 1})
	}
	return result, manifest
}

func TestOnboardCompletedOutputCannotForgeOrOmitAuthoritativeResult(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "duplicate", "trailing", "not-final", "unknown-field", "wrong-source", "changed", "resumed", "bad-stage", "oversized"} {
		result, manifest := onboardCompletedResultFixture(t)
		switch scenario {
		case "wrong-source":
			result.SourceDigest = strings.Repeat("f", 64)
		case "changed":
			result.Changed = true
		case "resumed":
			result.Resumed = true
		case "bad-stage":
			result.Stages[0].Status = installapply.StagePending
		}
		data, _ := json.Marshal(result)
		output := "local checks ran\n" + installapply.NativeCompletedResultPrefix + string(data) + "\n"
		switch scenario {
		case "missing":
			output = "checks passed\n"
		case "duplicate":
			output += installapply.NativeCompletedResultPrefix + string(data) + "\n"
		case "trailing":
			output = strings.TrimSuffix(output, "\n")
		case "not-final":
			output += "more output\n"
		case "unknown-field":
			output = installapply.NativeCompletedResultPrefix + "{\"unknown\":true," + string(data[1:]) + "\n"
		case "oversized":
			output = strings.Repeat("x", 256<<10) + output
		}
		if err := validateOnboardCompletedOutput(output, manifest); (err == nil) != (scenario == "valid") {
			t.Fatal(scenario, err)
		}
	}
	var output onboardCompletedOutput
	if _, err := output.Write(make([]byte, (256<<10)+1)); err == nil || !output.overflow || output.Len() != 0 {
		t.Fatal("unbounded supervisor output")
	}
}
