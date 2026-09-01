// SPDX-License-Identifier: AGPL-3.0-or-later

package ociresources

import (
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
)

func TestNormalizeProducesStableMetadataOnlyResourceIntent(t *testing.T) {
	t.Parallel()
	identity := testIdentity(t)
	spec := Spec{
		Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455da", Revision: 3,
		EnvironmentReferences: []EnvironmentReference{
			{SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", Environment: "TOKEN", Generation: 4},
			{SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Environment: "DATABASE_URL", Generation: 2},
		},
		Volumes: []ociapps.VolumeMount{
			{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455de", ContainerPath: "/var/lib/app"},
			{VolumeID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dd", ContainerPath: "/config", ReadOnly: true},
		},
	}
	normalized, err := Normalize(spec)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.EnvironmentReferences[0].Environment != "DATABASE_URL" || normalized.EnvironmentReferences[0].Generation != 2 ||
		normalized.Volumes[0].ContainerPath != "/config" {
		t.Fatalf("normalized = %#v", normalized)
	}
	if err := Validate(normalized); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	result, err := ResultFor(normalized, true)
	if err != nil || ValidateResult(result) != nil || result.EnvironmentReferenceCount != 2 || result.VolumeCount != 2 {
		t.Fatalf("ResultFor = %#v, %v", result, err)
	}
	values, err := IdentityInvocationValues(identity)
	decoded, decodeErr := IdentityFromInvocationValues(values)
	if err != nil || decodeErr != nil || !reflect.DeepEqual(decoded, identity) {
		t.Fatalf("identity invocation round trip = %#v, %v/%v", decoded, err, decodeErr)
	}
	if path, err := VolumePath(identity, normalized.Volumes[0].VolumeID); err != nil ||
		path != identity.HomeDirectory+"/"+VolumeRootName+"/"+normalized.Volumes[0].VolumeID {
		t.Fatalf("VolumePath = %q, %v", path, err)
	}
}

func TestValidateRejectsGenerationAndOrderingDrift(t *testing.T) {
	t.Parallel()
	spec := Spec{
		Identity: testIdentity(t), ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455da", Revision: 1,
		EnvironmentReferences: []EnvironmentReference{{
			SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Environment: "TOKEN", Generation: 0,
		}},
	}
	if Validate(spec) == nil {
		t.Fatal("zero secret generation was accepted")
	}
	spec.EnvironmentReferences[0].Generation = 1
	spec.EnvironmentReferences = append(spec.EnvironmentReferences, EnvironmentReference{
		SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455dc", Environment: "DATABASE_URL", Generation: 1,
	})
	if Validate(spec) == nil {
		t.Fatal("non-canonical secret order was accepted")
	}
}

func testIdentity(t *testing.T) hostingidentity.Spec {
	t.Helper()
	accountID := "019d2eaa-52d0-7f52-8ac7-0aeb932455d9"
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	return hostingidentity.Spec{AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home}
}
