// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
)

func main() { fmt.Print(cacheconfig.ManagedVCL()) }
