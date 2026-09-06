// SPDX-License-Identifier: AGPL-3.0-or-later

package cacheconfig

const FastCGIDirectory = "/var/cache/stackfort-fastcgi"

// FastCGIGlobal is fixed http-context configuration, not tenant-provided text.
// max_size/min_free are asynchronous cache-manager bounds, not tenant quotas.
func FastCGIGlobal() string {
	return `
    fastcgi_cache_path /var/cache/stackfort-fastcgi levels=1:2
        keys_zone=StackfortFastCGI:16m max_size=512m min_free=1g inactive=10m use_temp_path=off;

    # Only bodyless anonymous GETs without query strings or cache overrides.
    map "$request_method|$http_cookie|$http_authorization|$http_cache_control|$http_pragma|$http_range|$http_content_length|$http_transfer_encoding|$args" $stackfort_fastcgi_request_bypass {
        default 1;
        "GET||||||||" 0;
        "GET||||||0||" 0;
    }
    # Match the ORIGINAL request URI, before front-controller rewrites.
    map $request_uri $stackfort_fastcgi_path_bypass {
        default 0;
        ~% 1;
        "~*(^|/)(wp-admin|wp-login[.]php|wp-json|xmlrpc[.]php|wp-cron[.]php|admin|login|logout|signin|signup|account|my-account|cart|checkout|api)(/|[.?;]|$)" 1;
    }
    map $upstream_status $stackfort_fastcgi_response_bypass {
        default 1;
        200 0;
    }
    map $upstream_http_cache_control $stackfort_fastcgi_origin_bypass {
        default 1;
        "~*(^|,)[ ]*(s-maxage|max-age)=[1-9][0-9]*([ ]*,|[ ]*$)" 0;
    }
    map $upstream_http_x_stackfort_cache $stackfort_vinyl_cache_status {
        default "";
        HIT HIT;
        MISS MISS;
        BYPASS BYPASS;
    }
    map $upstream_cache_status $stackfort_cache_status {
        default $stackfort_vinyl_cache_status;
        HIT HIT;
        MISS MISS;
        BYPASS BYPASS;
        EXPIRED MISS;
        REVALIDATED MISS;
        STALE HIT;
        UPDATING HIT;
    }
`
}
