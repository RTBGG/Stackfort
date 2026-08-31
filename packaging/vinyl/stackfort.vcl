vcl 4.1;

backend default {
    .host = "127.0.0.1";
    .port = "9000";
    .connect_timeout = 1s;
    .first_byte_timeout = 30s;
    .between_bytes_timeout = 5s;
}

sub vcl_recv {
    if (req.http.X-Stackfort-Cache-Preset != "respect_origin" &&
        req.http.X-Stackfort-Cache-Preset != "wordpress") {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if (req.method != "GET" && req.method != "HEAD") {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if ((req.http.Content-Length && req.http.Content-Length != "0") || req.http.Transfer-Encoding) {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if (req.http.Authorization || req.http.Cookie) {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if (req.url ~ "^/(?:wp-admin(?:/|$)|wp-login\\.php(?:/|\\?|$)|admin(?:/|$)|login(?:/|$)|account(?:/|$)|cart(?:/|$)|checkout(?:/|$)|api(?:/|$))") {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    return (hash);
}

sub vcl_hash {
    hash_data(req.http.host);
    hash_data(req.url);
}

sub vcl_backend_response {
    set beresp.grace = 30s;
    if (beresp.http.Set-Cookie || beresp.http.Cache-Control ~ "(?i)(private|no-store|no-cache)" ||
        beresp.status < 200 || beresp.status >= 400) {
        set beresp.uncacheable = true;
        set beresp.ttl = 0s;
        return (deliver);
    }
    if (bereq.http.X-Stackfort-Cache-Preset == "respect_origin" &&
        beresp.http.Cache-Control !~ "(?i)(s-maxage|max-age)") {
        set beresp.uncacheable = true;
        set beresp.ttl = 0s;
        return (deliver);
    }
    if (bereq.http.X-Stackfort-Cache-Preset == "wordpress" && beresp.ttl <= 0s) {
        set beresp.ttl = 120s;
    }
}

sub vcl_deliver {
    if (req.http.X-Stackfort-Cache-Decision == "BYPASS") {
        set resp.http.X-Stackfort-Cache = "BYPASS";
    } else if (obj.hits > 0) {
        set resp.http.X-Stackfort-Cache = "HIT";
    } else {
        set resp.http.X-Stackfort-Cache = "MISS";
    }
    unset resp.http.Via;
    unset resp.http.X-Varnish;
}
