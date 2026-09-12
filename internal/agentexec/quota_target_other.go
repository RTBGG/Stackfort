// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package agentexec

func projectQuotaTarget() (string, error) { return "/srv/hosting", nil }
