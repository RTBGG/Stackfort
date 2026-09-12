// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/RTBGG/stackfort/internal/installapply"
)

type onboardPublicController struct {
	inspect func(context.Context, installapply.NativeOnboardingSource) (installapply.NativeOnboardingPrepared, bool, error)
	fresh   func(context.Context, installapply.NativeOnboardingSource, io.Writer) error
	check   func(context.Context, installapply.NativeOnboardingPrepared) error
}

type onboardCompletedOutput struct {
	bytes.Buffer
	overflow bool
}

func (output *onboardCompletedOutput) Write(data []byte) (int, error) {
	if output.Len()+len(data) > 256<<10 {
		output.overflow = true
		return 0, errors.New("completed native output exceeded its bound")
	}
	return output.Buffer.Write(data)
}

func validateOnboardCompletedOutput(output string, manifest installapply.NativeReleaseManifest) error {
	if len(output) > 256<<10 || !strings.HasSuffix(output, "\n") || strings.Count(output, installapply.NativeCompletedResultPrefix) != 1 {
		return errors.New("missing or ambiguous supervised native completion result")
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, installapply.NativeCompletedResultPrefix) {
		return errors.New("supervised native result is not final")
	}
	data := strings.TrimPrefix(last, installapply.NativeCompletedResultPrefix)
	if len(data) > 64<<10 {
		return errors.New("supervised native result exceeded bound")
	}
	var result installapply.Result
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return errors.New("invalid supervised native result")
	}
	canonical, err := json.Marshal(result)
	if err != nil || string(canonical) != data {
		return errors.New("noncanonical supervised native result")
	}
	return installapply.ValidateNativeCompletedResult(result, manifest)
}

func (controller onboardPublicController) run(ctx context.Context, selection installapply.NativeOnboardingSource, output io.Writer) error {
	if ctx == nil || ctx.Err() != nil || selection.Validate() != nil || output == nil {
		return errors.New("invalid public onboarding invocation")
	}
	prepared, complete, err := controller.inspect(ctx, selection)
	if err != nil {
		return err
	}
	if !complete {
		if prepared != (installapply.NativeOnboardingPrepared{}) {
			return errors.New("inconsistent native completion result")
		}
		return controller.fresh(ctx, selection, output)
	}
	if prepared.Manifest.Release.Policy != selection.Origin || prepared.RuntimePath != installapply.NativeRuntimePath ||
		prepared.OperationID != prepared.Manifest.Release.Source.OperationID || prepared.InstallerSHA256 != prepared.Manifest.Release.Source.InstallerSHA256 {
		return errors.New("completed native handoff differs from selected tagged installation")
	}
	if _, err := prepared.Manifest.Plan(); err != nil {
		return err
	}
	if err := onboardWrite(output, "Existing completed native installation found. Rechecking live storage, installed payload and service admission; no setup code is reissued and no reboot is scheduled.\n"); err != nil {
		return err
	}
	if err := controller.check(ctx, prepared); err != nil {
		return err
	}
	return onboardWrite(output, "Stackfort is already installed. Live native admission checks passed; the original administrator/setup credentials remain unchanged.\n")
}
