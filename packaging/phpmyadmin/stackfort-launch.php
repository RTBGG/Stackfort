<?php
// SPDX-License-Identifier: AGPL-3.0-or-later
// Managed by Stackfort. Do not edit.

declare(strict_types=1);

const STACKFORT_PMA_HANDOFF_COOKIE = 'stackfort_pma_handoff';

header('Cache-Control: no-store');
header('Content-Security-Policy: default-src \'none\'; frame-ancestors \'none\'');
header('Referrer-Policy: no-referrer');
header('X-Content-Type-Options: nosniff');

if (($_SERVER['REQUEST_METHOD'] ?? '') !== 'POST') {
    header('Location: /', true, 303);
    exit;
}
if (strcasecmp((string) ($_SERVER['HTTP_SEC_FETCH_SITE'] ?? ''), 'cross-site') === 0) {
    http_response_code(403);
    exit;
}

$handoffToken = $_POST['handoff_token'] ?? null;
if (! is_string($handoffToken) || preg_match('/^[A-Za-z0-9_-]{43}$/D', $handoffToken) !== 1) {
    http_response_code(400);
    exit;
}

setcookie(STACKFORT_PMA_HANDOFF_COOKIE, $handoffToken, [
    'expires' => time() + 30,
    'path' => '/phpmyadmin/',
    'secure' => true,
    'httponly' => true,
    'samesite' => 'Strict',
]);
header('Location: /phpmyadmin/index.php', true, 303);
exit;
