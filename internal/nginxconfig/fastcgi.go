// SPDX-License-Identifier: AGPL-3.0-or-later

package nginxconfig

import (
	"bytes"

	"github.com/RTBGG/stackfort/internal/core"
)

func writeFastCGICache(output *bytes.Buffer, item preparedDomain) {
	output.WriteString("        fastcgi_cache StackfortFastCGI;\n        fastcgi_cache_key ")
	output.WriteString(quoteDynamic(string(item.domain.AccountID)+":"+item.base+":"+string(item.domain.Cache.Generation)+":", "$scheme:$host:$request_uri"))
	output.WriteString(`;
        fastcgi_cache_methods GET;
        fastcgi_cache_bypass $stackfort_fastcgi_request_bypass $stackfort_fastcgi_path_bypass;
        fastcgi_no_cache $stackfort_fastcgi_request_bypass $stackfort_fastcgi_path_bypass $stackfort_fastcgi_response_bypass`)
	if item.cachePreset == core.CachePresetFastCGIRespectOrigin {
		output.WriteString(" $stackfort_fastcgi_origin_bypass")
	}
	output.WriteString(`;
        fastcgi_cache_valid 200 120s;
        fastcgi_cache_lock on;
        fastcgi_cache_lock_timeout 5s;
        fastcgi_cache_lock_age 5s;
        fastcgi_cache_background_update off;
        fastcgi_cache_use_stale off;
        # Preserve NGINX's Cache-Control, Expires, Set-Cookie and Vary handling.
        # Do not let an application override that policy using X-Accel-Expires.
        fastcgi_ignore_headers X-Accel-Expires;
        fastcgi_hide_header X-Stackfort-Cache;
        fastcgi_param HTTP_X_FORWARDED_HOST $host;
        fastcgi_param HTTP_X_FORWARDED_PROTO $scheme;
        fastcgi_param HTTP_X_FORWARDED_FOR $remote_addr;
        fastcgi_param HTTP_FORWARDED "";
        fastcgi_param HTTP_X_ORIGINAL_URL "";
        fastcgi_param HTTP_X_REWRITE_URL "";
`)
}
