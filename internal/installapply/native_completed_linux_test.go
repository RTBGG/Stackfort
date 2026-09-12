// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type nativeCompletedStageDouble struct {
	manifest NativeReleaseManifest
	complete bool
	closed   bool
	events   []string
	failure  string
}

func (stage *nativeCompletedStageDouble) event(name string) error {
	stage.events = append(stage.events, name)
	if stage.failure == name {
		return errors.New("injected " + name)
	}
	return nil
}
func (stage *nativeCompletedStageDouble) completedNativeManifest(context.Context) (NativeReleaseManifest, bool, error) {
	return stage.manifest, stage.complete, stage.event("records")
}
func (stage *nativeCompletedStageDouble) Close() error {
	stage.closed = true
	return stage.event("close")
}

func TestNativeCompletedInspectionCannotAdoptOrResumeAndClosesBeforeHandoff(t *testing.T) {
	for _, scenario := range []string{"valid", "absent", "empty", "self-error", "open-error", "records-error", "inputs-error", "close-error", "foreign-build", "self-digest", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			request, review := testNativeOnboarding(t)
			stage := &nativeCompletedStageDouble{complete: true, manifest: NativeReleaseManifest{SchemaVersion: 1, Release: review.Recovery.Release, Host: review.Recovery.Snapshot.Host, BootSHA256: strings.Repeat("e", 64)}}
			coordinator := nativeCompletedCoordinator{
				self: func(_ context.Context, policy OriginPolicy) (string, error) {
					if policy != request.Source.Origin {
						t.Fatal("self rebound")
					}
					digest := review.Recovery.Release.Source.InstallerSHA256
					if scenario == "self-digest" {
						digest = strings.Repeat("f", 64)
					}
					return digest, stage.event("self")
				},
				existing: func() (nativeCompletedStage, bool, error) { return stage, scenario != "absent", stage.event("open") },
				inputs: func(_ context.Context, selection NativeOnboardingSource, binding ReleaseBinding) error {
					if selection != request.Source || binding != review.Recovery.Release {
						t.Fatal("input binding changed")
					}
					return stage.event("inputs")
				},
			}
			ctx := t.Context()
			switch scenario {
			case "empty":
				stage.complete = false
			case "self-error":
				stage.failure = "self"
			case "open-error":
				stage.failure = "open"
			case "records-error":
				stage.failure = "records"
			case "inputs-error":
				stage.failure = "inputs"
			case "close-error":
				stage.failure = "close"
			case "foreign-build":
				stage.manifest.Release.Policy.Commit = strings.Repeat("f", 40)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, complete, err := coordinator.inspect(ctx, request.Source)
			if scenario == "valid" {
				if err != nil || !complete || !stage.closed || result.Manifest != stage.manifest || !reflect.DeepEqual(stage.events, []string{"self", "open", "records", "inputs", "close"}) {
					t.Fatal(result, complete, err, stage.events)
				}
			} else if scenario == "absent" || scenario == "empty" {
				if err != nil || complete || result != (NativeOnboardingPrepared{}) {
					t.Fatal(result, complete, err)
				}
			} else if err == nil || complete || result != (NativeOnboardingPrepared{}) {
				t.Fatal("unsafe completed handoff", scenario, result, complete, err)
			}
		})
	}
}

func TestNativeCompletedCleanupNeverTreatsSignalsOrForeignContextAsSuccess(t *testing.T) {
	unit, _ := NativeCompletedRecheckUnit(sourceOperation)
	cgroup := "0::/system.slice/" + unit + "\n"
	for _, values := range [][4]string{{cgroup, "success", "exited", "0"}, {cgroup, "success", "killed", "TERM"}, {cgroup, "signal", "killed", "KILL"}, {cgroup, "exit-code", "exited", "1"}, {cgroup, "", "", ""}, {"0::/user.slice/" + unit + "\n", "success", "exited", "0"}, {cgroup + "0::/foreign\n", "success", "exited", "0"}} {
		actual := nativeCompletedCleanupSucceeded(sourceOperation, values[0], values[1], values[2], values[3])
		expected := values == [4]string{cgroup, "success", "exited", "0"}
		if actual != expected {
			t.Fatal(values, actual)
		}
	}
}

func TestNativeCompletedInspectorRejectsUnlockedStage(t *testing.T) {
	for _, stage := range []*SourceStage{nil, {}, {closed: true}} {
		if result, complete, err := stage.completedNativeManifest(t.Context()); err == nil || complete || result != (NativeReleaseManifest{}) {
			t.Fatal(result, complete, err)
		}
	}
}
