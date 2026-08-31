<?php
// SPDX-License-Identifier: AGPL-3.0-or-later
// Managed by Stackfort. Do not edit.

declare(strict_types=1);

// phpcs:disable Squiz.Functions.GlobalFunction

/** @return array{0: string, 1: string} */
function get_login_credentials($configuredUser): array
{
    unset($configuredUser);
    $cookieName = 'stackfort_pma_handoff';
    $handoffToken = $_COOKIE[$cookieName] ?? null;
    setcookie($cookieName, '', [
        'expires' => 1,
        'path' => '/phpmyadmin/',
        'secure' => true,
        'httponly' => true,
        'samesite' => 'Strict',
    ]);
    unset($_COOKIE[$cookieName]);

    if (! is_string($handoffToken) || preg_match('/^[A-Za-z0-9_-]{43}$/D', $handoffToken) !== 1) {
        return ['', ''];
    }
    $sharedKey = @file_get_contents('/var/lib/stackfort-phpmyadmin-broker/broker.key');
    if (! is_string($sharedKey) || strlen($sharedKey) !== 32) {
        return ['', ''];
    }
    $mac = hash_hmac('sha256', "stackfort-phpmyadmin-broker-v1\n" . $handoffToken, $sharedKey, true);
    $authentication = rtrim(strtr(base64_encode($mac), '+/', '-_'), '=');
    $payload = json_encode(['handoffToken' => $handoffToken], JSON_THROW_ON_ERROR);

    $curl = curl_init('http://127.0.0.1:8081/v1/redeem');
    if ($curl === false) {
        return ['', ''];
    }
    curl_setopt_array($curl, [
        CURLOPT_POST => true,
        CURLOPT_POSTFIELDS => $payload,
        CURLOPT_HTTPHEADER => [
            'Content-Type: application/json',
            'X-Stackfort-PMA-Authentication: ' . $authentication,
        ],
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_CONNECTTIMEOUT_MS => 500,
        CURLOPT_TIMEOUT_MS => 2000,
        CURLOPT_PROXY => '',
    ]);
    $response = curl_exec($curl);
    $status = curl_getinfo($curl, CURLINFO_RESPONSE_CODE);
    curl_close($curl);
    if (! is_string($response) || $status !== 200 || strlen($response) > 4096) {
        return ['', ''];
    }

    try {
        $credential = json_decode($response, true, 4, JSON_THROW_ON_ERROR);
    } catch (Throwable $error) {
        unset($error);
        return ['', ''];
    }
    $username = $credential['username'] ?? null;
    $host = $credential['host'] ?? null;
    $encodedPassword = $credential['passwordBase64'] ?? null;
    if (! is_string($username) || preg_match('/^[a-z0-9_]{1,80}$/D', $username) !== 1 ||
        $host !== 'localhost' || ! is_string($encodedPassword)) {
        return ['', ''];
    }
    $password = base64_decode($encodedPassword, true);
    if (! is_string($password) || strlen($password) > 1024) {
        return ['', ''];
    }
    return [$username, $password];
}
